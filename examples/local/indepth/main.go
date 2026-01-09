package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luxfi/config"
	"github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	luxtls "github.com/luxfi/tls"
)

const (
	healthyTimeout    = 2 * time.Minute
	removeNodeTimeout = 10 * time.Second
)

var goPath = os.ExpandEnv("$GOPATH")

// Blocks until a signal is received on [signalChan], upon which
// [n.Stop()] is called. If [signalChan] is closed, does nothing.
// Closes [closedOnShutdownChan] amd [signalChan] when done shutting down network.
// This function should only be called once.
func shutdownOnSignal(
	logger log.Logger,
	n network.Network,
	signalChan chan os.Signal,
	closedOnShutdownChan chan struct{},
) {
	sig := <-signalChan
	logger.Info("got OS signal", log.Stringer("signal", sig))
	if err := n.Stop(context.Background()); err != nil {
		logger.Info("error stopping network", log.Err(err))
	}
	signal.Reset()
	close(signalChan)
	close(closedOnShutdownChan)
}

// Shows example usage of the Lux Network Runner.
// Creates a local five node Lux network
// and waits for all nodes to become healthy.
// Then, we:
// * print the names of the nodes
// * print the node ID of one node
// * start a new node
// * remove an existing node
// The network runs until the user provides a SIGINT or SIGTERM.
func main() {
	// Create the logger
	logFactory := log.NewFactoryWithConfig(log.Config{
		DisplayLevel: level.Info,
		LogLevel:     level.Debug,
	})
	logger, err := logFactory.Make("main")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	binaryPath := fmt.Sprintf("%s%s", goPath, "/src/github.com/luxfi/node/build/node")
	if err := run(logger, binaryPath); err != nil {
		logger.Fatal("fatal error", log.Err(err))
		os.Exit(1)
	}
}

func run(logger log.Logger, binaryPath string) error {
	// Create the network
	nw, err := local.NewDefaultNetwork(logger, binaryPath, true)
	if err != nil {
		return err
	}
	defer func() { // Stop the network when this function returns
		if err := nw.Stop(context.Background()); err != nil {
			logger.Info("error stopping network", log.Err(err))
		}
	}()

	// When we get a SIGINT or SIGTERM, stop the network and close [closedOnShutdownCh]
	signalsChan := make(chan os.Signal, 1)
	signal.Notify(signalsChan, syscall.SIGINT)
	signal.Notify(signalsChan, syscall.SIGTERM)
	closedOnShutdownCh := make(chan struct{})
	go func() {
		shutdownOnSignal(logger, nw, signalsChan, closedOnShutdownCh)
	}()

	// Wait until the nodes in the network are ready
	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()
	logger.Info("waiting for all nodes to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return err
	}

	// Print the node names
	nodeNames, err := nw.GetNodeNames()
	if err != nil {
		return err
	}
	logger.Info("current network's nodes", log.Strings("nodes", nodeNames))

	// Get one node
	node1, err := nw.GetNode(nodeNames[0])
	if err != nil {
		return err
	}

	// Get its node ID through its API and print it
	node1ID, _, err := (*node1.GetAPIClient().InfoAPI()).GetNodeID(context.Background())
	if err != nil {
		return err
	}
	logger.Info("one node's ID is", log.Stringer("nodeID", node1ID))

	// Add a new node with generated cert/key/nodeid
	stakingCert, stakingKey, err := luxtls.NewCertAndKeyBytes()
	if err != nil {
		return err
	}
	nodeConfig := node.Config{
		Name:        "New Node",
		BinaryPath:  binaryPath,
		StakingKey:  string(stakingKey),
		StakingCert: string(stakingCert),
		// The flags below would override the config in this node's config file,
		// if it had one.
		Flags: map[string]interface{}{
			config.LogLevelKey: log.Debug,
			config.HTTPHostKey: "0.0.0.0",
		},
	}
	if _, err := nw.AddNode(nodeConfig); err != nil {
		return err
	}

	// Remove one node
	nodeToRemove := nodeNames[3]
	logger.Info("removing node", log.String("name", nodeToRemove))
	removeNodeCtx, removeNodeCtxCancel := context.WithTimeout(context.Background(), removeNodeTimeout)
	defer removeNodeCtxCancel()
	if err := nw.RemoveNode(removeNodeCtx, nodeToRemove); err != nil {
		return err
	}

	// Wait until the nodes in the updated network are ready
	ctx, cancel = context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()
	logger.Info("waiting for updated network to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return err
	}

	// Print the node names
	nodeNames, err = nw.GetNodeNames()
	if err != nil {
		return err
	}
	// Will have the new node but not the removed one
	logger.Info("updated network's nodes", log.Strings("nodes", nodeNames))
	logger.Info("Network will run until you CTRL + C to exit...")
	// Wait until done shutting down network after SIGINT/SIGTERM
	<-closedOnShutdownCh
	return nil
}
