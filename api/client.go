package api

import (
	"github.com/luxfi/node/api/admin"
	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/indexer"
	"github.com/luxfi/node/vms/xvm"
	"github.com/luxfi/node/vms/platformvm"
	evmclient "github.com/luxfi/evm/plugin/evm/client"
)

// Issues API calls to a node
// TODO: byzantine api. check if appropriate. improve implementation.
type Client interface {
	PChainAPI() *platformvm.Client
	XChainAPI() *xvm.Client
	XChainWalletAPI() *xvm.WalletClient
	CChainAPI() evmclient.Client
	CChainEthAPI() EthClient // ethclient websocket wrapper that adds mutexed calls, and lazy conn init (on first call)
	InfoAPI() *info.Client
	HealthAPI() *health.Client
	AdminAPI() *admin.Client
	PChainIndexAPI() *indexer.Client
	CChainIndexAPI() *indexer.Client
	// TODO add methods
}
