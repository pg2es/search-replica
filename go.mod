module github.com/pg2es/search-replica

go 1.17

require (
	github.com/jackc/pgconn v1.9.1-0.20210724152538-d89c8390a530
	github.com/jackc/pglogrepl v0.0.0-20210628224733-3140d41f7881
	github.com/jackc/pgproto3/v2 v2.1.1
	github.com/jackc/pgtype v1.10.1-0.20220329203659-75dc53c3e8c2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/mailru/easyjson v0.7.7
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.2
	github.com/stretchr/testify v1.7.0
	go.uber.org/zap v1.21.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20200714003250-2b9c44734f2b // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sys v0.0.0-20220114195835-da31bd327af9 // indirect
	golang.org/x/text v0.3.6 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)

replace github.com/jackc/pgtype v1.10.1-0.20220329203659-75dc53c3e8c2 => github.com/pg2es/pgtype v1.10.1-0.20220329203659-75dc53c3e8c2

replace github.com/jackc/pglogrepl v0.0.0-20210628224733-3140d41f7881 => github.com/pg2es/pglogrepl v0.0.0-20220403033012-e6ee500b61d2
