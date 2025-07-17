package api

import (
	"github.com/luxfi/node/api/admin"
	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/api/ipcs"
	"github.com/luxfi/node/api/keystore"
	"github.com/luxfi/node/indexer"
	"github.com/luxfi/node/vms/avm"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/geth/plugin/evm"
)

// Issues API calls to a node
// TODO: byzantine api. check if appropriate. improve implementation.
type Client interface {
	PChainAPI() platformvm.Client
	XChainAPI() avm.Client
	XChainWalletAPI() avm.WalletClient
	CChainAPI() evm.Client
	CChainEthAPI() EthClient // ethclient websocket wrapper that adds mutexed calls, and lazy conn init (on first call)
	InfoAPI() info.Client
	HealthAPI() health.Client
	IpcsAPI() ipcs.Client
	KeystoreAPI() keystore.Client
	AdminAPI() admin.Client
	PChainIndexAPI() indexer.Client
	CChainIndexAPI() indexer.Client
	// TODO add methods
}
