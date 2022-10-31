package api

import (
	"github.com/luxdefi/luxd/api/admin"
	"github.com/luxdefi/luxd/api/health"
	"github.com/luxdefi/luxd/api/info"
	"github.com/luxdefi/luxd/api/ipcs"
	"github.com/luxdefi/luxd/api/keystore"
	"github.com/luxdefi/luxd/indexer"
	"github.com/luxdefi/luxd/vms/avm"
	"github.com/luxdefi/luxd/vms/platformvm"
	"github.com/luxdefi/coreth/plugin/evm"
)

// Issues API calls to a node
// TODO: byzantine api. check if appropiate. improve implementation.
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
