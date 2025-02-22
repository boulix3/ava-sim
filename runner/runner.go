package runner

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/ava-labs/ava-sim/constants"
	"github.com/ava-labs/ava-sim/manager"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformStatus "github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/fatih/color"
)

const (
	genesisKey   = "PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	waitTime     = 1 * time.Second
	longWaitTime = 10 * waitTime

	validatorWeight    = 50
	validatorStartDiff = 30 * time.Second
	validatorEndDiff   = 30 * 24 * time.Hour // 30 days
)

func SetupSubnet(ctx context.Context, vmGenesis string) error {
	color.Cyan("creating subnet")
	var (
		nodeURLs = manager.NodeURLs()
		nodeIDs  = manager.NodeIDs()

		userPass = api.UserPass{
			Username: "test",
			Password: "vmsrkewl",
		}
	)

	// Create user
	kclient := keystore.NewClient(nodeURLs[0])
	ok, err := kclient.CreateUser(ctx, userPass)
	if !ok || err != nil {
		return fmt.Errorf("could not create user: %w", err)
	}

	// Connect to local network
	client := platformvm.NewClient(nodeURLs[0])

	// Import genesis key
	fundedAddress, err := client.ImportKey(ctx, userPass, genesisKey)
	if err != nil {
		return fmt.Errorf("unable to import genesis key: %w", err)
	}
	balance, err := client.GetBalance(ctx, []string{fundedAddress})
	if err != nil {
		return fmt.Errorf("unable to get genesis key balance: %w", err)
	}
	color.Cyan("found %d on address %s", balance, fundedAddress)

	// Create a subnet
	subnetIDTx, err := client.CreateSubnet(
		ctx,
		userPass,
		[]string{fundedAddress},
		fundedAddress,
		[]string{fundedAddress},
		1,
	)
	if err != nil {
		return fmt.Errorf("unable to create subnet: %w", err)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, _ := client.GetTxStatus(ctx, subnetIDTx, true)
		if status.Status == platformStatus.Committed {
			break
		}
		color.Yellow("waiting for subnet creation tx (%s) to be accepted", subnetIDTx)
		time.Sleep(waitTime)
	}
	color.Cyan("subnet creation tx (%s) accepted", subnetIDTx)

	// Confirm created subnet appears in subnet list
	subnets, err := client.GetSubnets(ctx, []ids.ID{})
	if err != nil {
		return fmt.Errorf("cannot query subnets: %w", err)
	}
	rSubnetID := subnets[0].ID
	subnetID := rSubnetID.String()
	if subnetID != constants.WhitelistedSubnets {
		return fmt.Errorf("expected subnet %s but got %s", constants.WhitelistedSubnets, subnetID)
	}

	// Add all validators to subnet with equal weight
	for _, nodeID := range manager.NodeIDs() {
		txID, err := client.AddSubnetValidator(
			ctx,
			userPass, []string{fundedAddress}, fundedAddress,
			subnetID, nodeID, validatorWeight,
			uint64(time.Now().Add(validatorStartDiff).Unix()),
			uint64(time.Now().Add(validatorEndDiff).Unix()),
		)
		if err != nil {
			return fmt.Errorf("unable to add subnet validator: %w", err)
		}

		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status, _ := client.GetTxStatus(ctx, txID, true)
			if status.Status == platformStatus.Committed {
				break
			}
			color.Yellow("waiting for add subnet validator (%s) tx (%s) to be accepted", nodeID, txID)
			time.Sleep(waitTime)
		}
		color.Cyan("add subnet validator (%s) tx (%s) accepted", nodeID, txID)
	}

	// Create blockchain
	genesis, err := ioutil.ReadFile(vmGenesis)
	if err != nil {
		return fmt.Errorf("could not read genesis file (%s): %w", vmGenesis, err)
	}
	txID, err := client.CreateBlockchain(
		ctx,
		userPass, []string{fundedAddress}, fundedAddress, rSubnetID,
		constants.VMID, []string{}, constants.VMName, genesis,
	)
	if err != nil {
		return fmt.Errorf("could not create blockchain: %w", err)
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, _ := client.GetTxStatus(ctx, txID, true)
		if status.Status == platformStatus.Committed {
			break
		}
		color.Yellow("waiting for create blockchain tx (%s) to be accepted", txID)
		time.Sleep(waitTime)
	}
	color.Cyan("create blockchain tx (%s) accepted", txID)

	// Validate blockchain exists
	blockchains, err := client.GetBlockchains(ctx)
	if err != nil {
		return fmt.Errorf("could not query blockchains: %w", err)
	}
	var blockchainID ids.ID
	for _, blockchain := range blockchains {
		if blockchain.SubnetID == rSubnetID {
			blockchainID = blockchain.ID
			break
		}
	}
	if blockchainID == (ids.ID{}) {
		return errors.New("could not find blockchain")
	}

	// Ensure all nodes are validating subnet
	for i, url := range nodeURLs {
		nClient := platformvm.NewClient(url)
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status, _ := nClient.GetBlockchainStatus(ctx, blockchainID.String())
			if status == platformStatus.Validating {
				break
			}
			color.Yellow("waiting for validating status for %s", nodeIDs[i])
			time.Sleep(longWaitTime)
		}
		color.Cyan("%s validating blockchain %s", nodeIDs[i], blockchainID)
	}

	// Ensure network bootstrapped
	for i, url := range nodeURLs {
		nClient := info.NewClient(url)
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			bootstrapped, _ := nClient.IsBootstrapped(ctx, blockchainID.String())
			if bootstrapped {
				break
			}
			color.Yellow("waiting for %s to bootstrap %s", nodeIDs[i], blockchainID.String())
			time.Sleep(waitTime)
		}
		color.Cyan("%s bootstrapped %s", nodeIDs[i], blockchainID)
	}

	// Print endpoints where VM is accessible
	color.Green("Custom VM endpoints now accessible at:")
	for i, url := range nodeURLs {
		color.Green("%s: %s/ext/bc/%s", nodeIDs[i], url, blockchainID.String())
	}
	return nil
}
