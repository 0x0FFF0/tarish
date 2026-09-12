module tarish

go 1.24.0

require (
	github.com/missuo/opensnell v1.0.4
	golang.org/x/mod v0.14.0
	golang.org/x/term v0.38.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

replace github.com/missuo/opensnell => ./third_party/opensnell
