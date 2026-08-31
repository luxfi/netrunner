module github.com/luxfi/netrunner

go 1.26.5

exclude google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1

exclude github.com/luxfi/geth v1.16.1

require (
	github.com/btcsuite/btcd v0.25.0
	github.com/btcsuite/btcd/btcutil v1.1.6
	github.com/luxfi/address v1.1.1
	github.com/luxfi/api v1.1.14
	github.com/luxfi/atomic v1.0.0
	github.com/luxfi/config v1.1.2
	github.com/luxfi/constants v1.6.4
	github.com/luxfi/crypto v1.20.5
	github.com/luxfi/genesis v1.16.19
	github.com/luxfi/geth v1.20.2
	github.com/luxfi/go-bip39 v1.2.0
	github.com/luxfi/ids v1.3.4
	github.com/luxfi/keys v1.4.2
	github.com/luxfi/kms v1.12.10
	github.com/luxfi/log v1.4.3
	github.com/luxfi/math v1.5.1
	github.com/luxfi/metric v1.10.1
	github.com/luxfi/node v1.36.179
	github.com/luxfi/p2p v1.22.1
	github.com/luxfi/proto v1.4.10
	github.com/luxfi/sdk v1.18.10
	github.com/luxfi/utxo v0.5.10
	github.com/luxfi/validators v1.3.3
	github.com/luxfi/version v1.0.1
	github.com/luxfi/zap v1.2.7
	github.com/luxfi/zapdb v1.10.6
	github.com/luxfi/zwing v0.6.1
	github.com/onsi/ginkgo/v2 v2.29.0
	github.com/onsi/gomega v1.41.0
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	go.uber.org/multierr v1.11.0
	golang.org/x/crypto v0.54.0
	golang.org/x/mod v0.37.0
	golang.org/x/sync v0.22.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260825221802-da73d73af1c5
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.36.1
	k8s.io/apimachinery v0.36.1
	k8s.io/client-go v0.36.1
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/ALTree/bigfloat v0.2.0 // indirect
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.5 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.8 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.13 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.13 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.22 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.97.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.10 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
	github.com/btcsuite/btcd/chainhash/v2 v2.0.0 // indirect
	github.com/btcsuite/btcutil v1.0.2 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cockroachdb/errors v1.13.0 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240816210425-c5d0cb0b6fc0 // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/pebble v1.1.5 // indirect
	github.com/cockroachdb/redact v1.1.8 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20250429170803-42689b6311bb // indirect
	github.com/cronokirby/saferith v0.33.0 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.1.0 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/getsentry/sentry-go v0.46.2 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260601182631-00ed12fed2a6 // indirect
	github.com/go-openapi/jsonpointer v0.21.2 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/gofiber/schema v1.7.1 // indirect
	github.com/gofiber/utils/v2 v2.0.4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/gtank/merlin v0.1.1 // indirect
	github.com/gtank/ristretto255 v0.2.0 // indirect
	github.com/hanzoai/vfs v0.4.3 // indirect
	github.com/hanzos3/crc64nvme v1.1.2 // indirect
	github.com/hanzos3/go v1.0.2 // indirect
	github.com/hanzos3/md5-simd v1.1.3 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/luxfi/accel v1.3.1 // indirect
	github.com/luxfi/age v1.6.0 // indirect
	github.com/luxfi/chains v1.7.33 // indirect
	github.com/luxfi/codec v1.2.1 // indirect
	github.com/luxfi/compress v0.1.1 // indirect
	github.com/luxfi/concurrent v0.1.1 // indirect
	github.com/luxfi/consensus v1.36.81 // indirect
	github.com/luxfi/container v0.2.2 // indirect
	github.com/luxfi/crypto/ipa v1.2.4 // indirect
	github.com/luxfi/dkg v0.3.5 // indirect
	github.com/luxfi/filesystem v0.0.1 // indirect
	github.com/luxfi/formatting v1.1.3 // indirect
	github.com/luxfi/lattice/v7 v7.1.4 // indirect
	github.com/luxfi/light v1.0.0 // indirect
	github.com/luxfi/magnetar v1.2.3 // indirect
	github.com/luxfi/mdns v0.1.1 // indirect
	github.com/luxfi/mlwe v0.3.0 // indirect
	github.com/luxfi/net v0.1.1 // indirect
	github.com/luxfi/pq v1.1.0 // indirect
	github.com/luxfi/runtime v1.3.1 // indirect
	github.com/luxfi/staking v1.6.1 // indirect
	github.com/luxfi/sys v0.1.0 // indirect
	github.com/luxfi/timer v1.1.1 // indirect
	github.com/luxfi/utils v1.3.1 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/mimoo/StrobeGo v0.0.0-20220103164710-9a04d6ca976b // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/montanaflynn/stats v0.9.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pires/go-proxyproto v0.11.0 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.73.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zap-proto/fiber/v3 v3.2.1 // indirect
	github.com/zap-proto/go v1.3.0 // indirect
	github.com/zap-proto/http v0.3.5 // indirect
	github.com/zap-proto/mcp v1.0.5 // indirect
	github.com/zap-proto/zip v1.36.38 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.mongodb.org/mongo-driver v1.17.9 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260317180543-43fb72c5454a // indirect
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.5.0 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/consensys/gnark-crypto v0.20.1 // indirect
	github.com/crate-crypto/go-eth-kzg v1.5.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.9.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/dgraph-io/ristretto/v2 v2.4.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.7 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/golang/mock v1.7.0-rc.1 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/renameio/v2 v2.0.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/rpc v1.2.1 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.19.1
	github.com/luxfi/cache v1.3.1 // indirect
	github.com/luxfi/corona v0.10.4 // indirect
	github.com/luxfi/database v1.21.5 // indirect
	github.com/luxfi/go-bip32 v1.1.0 // indirect
	github.com/luxfi/keychain v1.1.1 // indirect
	github.com/luxfi/lens v0.2.1 // indirect
	github.com/luxfi/math/big v0.1.0 // indirect
	github.com/luxfi/mock v0.1.1 // indirect
	github.com/luxfi/pulsar v1.9.2 // indirect
	github.com/luxfi/rpc v1.1.0
	github.com/luxfi/sampler v1.1.0 // indirect
	github.com/luxfi/threshold v1.12.6 // indirect
	github.com/luxfi/tls v1.1.1
	github.com/luxfi/trace v1.2.1 // indirect
	github.com/luxfi/upgrade v1.0.3 // indirect
	github.com/luxfi/vm v1.3.16
	github.com/luxfi/warp v1.24.1 // indirect
	github.com/mr-tron/base58 v1.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.3.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.68.0 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/supranational/blst v0.3.16 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20260529124908-c761662dc8c9 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)
