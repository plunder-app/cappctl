package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/plunder-app/plunder/pkg/apiserver"
	"github.com/plunder-app/plunder/pkg/plunderlogging"
	"github.com/plunder-app/plunder/pkg/services"
	"github.com/spf13/cobra"
)

var logLevel int

// leaveDeploymentFlag - will stop the removal of the deployment once it's been destroyed
var leaveDeploymentFlag bool

var managermentCluster struct {
	mac     string
	address string
}

var machine struct {
	address string
}

func init() {
	cappctlCmd.PersistentFlags().IntVar(&logLevel, "logLevel", int(log.InfoLevel), "Set the logging level [0=panic, 3=warning, 5=debug]")

	initClusterCmd.Flags().StringVarP(&managermentCluster.mac, "mac", "m", "", "The Mac address of the node to use for provisioning")
	initClusterCmd.Flags().StringVarP(&managermentCluster.address, "address", "a", "", "The IP address to provision the management cluster with")

	destroyMachine.Flags().StringVarP(&machine.address, "address", "a", "", "Address of a machine to destroy")
	destroyMachine.Flags().BoolVarP(&leaveDeploymentFlag, "leave", "l", false, "Leave the deployment after it's been destroyed")

	cappctlCmd.AddCommand(destroyMachine)
	cappctlCmd.AddCommand(initClusterCmd)
}

func main() {
	if err := cappctlCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var cappctlCmd = &cobra.Command{
	Use:   "cappctl",
	Short: "Cluster API Plunder control",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
		return
	},
}

var initClusterCmd = &cobra.Command{
	Use:   "init-mgmt-cluster",
	Short: "Initialise Kubernetes Management Cluster",
	Run: func(cmd *cobra.Command, args []string) {
		log.SetLevel(log.Level(logLevel))

		fmt.Println("Beginning the deployment of a new host")

		if managermentCluster.mac == "" {
			// Print a warning
			fmt.Printf("Will select an unleased server at random for management cluster in 5 seconds\n")
			// Wait to give the user time to cancel with ctrl+c
			time.Sleep(5 * time.Second)
			// Get a mac address
			u, c, err := apiserver.BuildEnvironmentFromConfig("plunderclient.yaml", "")
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			u.Path = path.Join(u.Path, apiserver.DHCPAPIPath()+"/unleased")

			response, err := apiserver.ParsePlunderGet(u, c)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}
			// If an error has been returned then handle the error gracefully and terminate
			if response.FriendlyError != "" || response.Error != "" {
				log.Fatalf("%s", err.Error())

			}
			var unleased []services.Lease

			err = json.Unmarshal(response.Payload, &unleased)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			// Iterate through all known addresses and find a free one that looks "recent"
			for i := range unleased {
				if time.Since(unleased[i].Expiry).Minutes() < 10 {
					managermentCluster.mac = unleased[i].Nic
				}
			}

			// Hopefully we found one!
			if managermentCluster.mac == "" {
				log.Fatalf("No free hardware could be found to provison")
			}
		}

		u, c, err := apiserver.BuildEnvironmentFromConfig("plunderclient.yaml", "")
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		d := services.DeploymentConfig{
			ConfigName: "preseed",
			MAC:        managermentCluster.mac,
			ConfigHost: services.HostConfig{
				IPAddress:  managermentCluster.address,
				ServerName: "Manager01",
			},
		}

		u.Path = apiserver.DeploymentAPIPath()
		b, err := json.Marshal(d)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}
		response, err := apiserver.ParsePlunderPost(u, c, b)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}
		// If an error has been returned then handle the error gracefully and terminate
		if response.FriendlyError != "" || response.Error != "" {
			log.Debugln(response.Error)
			log.Fatalln(response.FriendlyError)
		}

		newMap := uptimeCommand(managermentCluster.address)

		// Marshall the parlay submission (runs the uptime command)
		b, err = json.Marshal(newMap)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		// Create the string that will be used to get the logs
		dashAddress := strings.Replace(managermentCluster.address, ".", "-", -1)

		// Get the time
		t := time.Now()

		for {
			// Set Parlay API path and POST
			u.Path = apiserver.ParlayAPIPath()
			response, err := apiserver.ParsePlunderPost(u, c, b)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			// If an error has been returned then handle the error gracefully and terminate
			if response.FriendlyError != "" || response.Error != "" {
				log.Debugln(response.Error)
				log.Fatalln(response.FriendlyError)
			}

			// Sleep for five seconds
			time.Sleep(5 * time.Second)

			// Set the parlay API get logs path and GET
			u.Path = apiserver.ParlayAPIPath() + "/logs/" + dashAddress
			response, err = apiserver.ParsePlunderGet(u, c)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}
			// If an error has been returned then handle the error gracefully and terminate
			if response.FriendlyError != "" || response.Error != "" {
				log.Debugln(response.Error)
				log.Fatalln(response.FriendlyError)
			}

			var logs plunderlogging.JSONLog

			err = json.Unmarshal(response.Payload, &logs)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			if logs.State == "Completed" {
				fmt.Printf("\r\033[32mHost has been succesfully provisioned OS in\033[m %s Seconds\n", time.Since(t).Round(time.Second))
				break
			} else {
				fmt.Printf("\r\033[36mWaiting for Host to complete OS provisioning \033[m%.0f Seconds", time.Since(t).Seconds())
			}
		}

		fmt.Printf("This process can be exited with ctrl+c and monitored with pldrctl get logs %s -w 5\n", managermentCluster.address)

		// Begin the Kubernetes installation //
		fmt.Println("Beginning the installation and initialisation of Kubernetes")

		// Get the Kubernetes Installation commands
		kubeMap := kubeCreateHostCommand(managermentCluster.address)

		// Add the kubeadm steps
		kubeMap.Deployments[0].Actions = append(kubeMap.Deployments[0].Actions, kubeadmActions()...)

		// Marshall the parlay submission (runs the uptime command)
		b, err = json.Marshal(kubeMap)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}
		// Set Parlay API path and POST
		u.Path = apiserver.ParlayAPIPath()
		response, err = apiserver.ParsePlunderPost(u, c, b)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		// If an error has been returned then handle the error gracefully and terminate
		if response.FriendlyError != "" || response.Error != "" {
			log.Debugln(response.Error)
			log.Fatalln(response.FriendlyError)
		}
		// Get the time
		t = time.Now()

		for {

			// Sleep for five seconds
			time.Sleep(5 * time.Second)

			// Set the parlay API get logs path and GET
			u.Path = apiserver.ParlayAPIPath() + "/logs/" + dashAddress
			response, err = apiserver.ParsePlunderGet(u, c)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}
			// If an error has been returned then handle the error gracefully and terminate
			if response.FriendlyError != "" || response.Error != "" {
				log.Debugln(response.Error)
				log.Fatalln(response.FriendlyError)
			}

			var logs plunderlogging.JSONLog

			err = json.Unmarshal(response.Payload, &logs)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			if logs.State == "Completed" {
				fmt.Printf("\r\033[32mKubernetes has been succesfully installed on host %s in\033[m %s Seconds\n", managermentCluster.address, time.Since(t).Round(time.Second))
				break
			} else if logs.State == "Failed" {
				log.Fatalln("Kubernetes has failed to install")
			} else {
				fmt.Printf("\r\033[36mWaiting for Kubernetes to complete installation \033[m%.0f Seconds", time.Since(t).Seconds())
			}
		}
		return
	},
}

var destroyMachine = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy a machine",
	Run: func(cmd *cobra.Command, args []string) {
		log.SetLevel(log.Level(logLevel))

		fmt.Println("Destroying a node")

		if machine.address == "" {
			// Print a warning and exit
			cmd.Help()
			log.Fatalf("No address specified for host")

		}

		u, c, err := apiserver.BuildEnvironmentFromConfig("plunderclient.yaml", "")
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		destroyMap := destroyCommand(machine.address)

		// Marshall the parlay submission (runs the uptime command)
		b, err := json.Marshal(destroyMap)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		u.Path = apiserver.ParlayAPIPath()
		response, err := apiserver.ParsePlunderPost(u, c, b)
		if err != nil {
			log.Fatalf("%s", err.Error())
		}

		// If an error has been returned then handle the error gracefully and terminate
		if response.FriendlyError != "" || response.Error != "" {
			log.Debugln(response.Error)
			log.Fatalln(response.FriendlyError)
		}

		fmt.Println("Node will now reset (through sysrq)")

		if !leaveDeploymentFlag {
			fmt.Println("Removing node from configuration so it wont be re-provisioned")
			u.Path = apiserver.DeploymentAPIPath() + "/address/" + strings.Replace(machine.address, ".", "-", -1)
			response, err = apiserver.ParsePlunderDelete(u, c)
			if err != nil {
				log.Fatalf("%s", err.Error())
			}

			// If an error has been returned then handle the error gracefully and terminate
			if response.FriendlyError != "" || response.Error != "" {
				log.Debugln(response.Error)
				log.Fatalln(response.FriendlyError)
			}
		}
		return
	},
}
