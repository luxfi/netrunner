package api

import (
	"github.com/luxdefi/node/api/admin"
	"github.com/luxdefi/node/api/health"
	"github.com/luxdefi/node/api/info"
	"github.com/luxdefi/node/api/ipcs"
	"github.com/luxdefi/node/api/keystore"
	"github.com/luxdefi/node/indexer"
	"github.com/luxdefi/node/vms/avm"
	"github.com/luxdefi/node/vms/platformvm"
	"github.com/luxdefi/coreth/plugin/evm"
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
