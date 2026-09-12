module tarish-server

go 1.24.0

require (
	github.com/mattn/go-sqlite3 v1.14.24
	tarish v0.0.0
)

require (
	github.com/missuo/opensnell v1.0.4 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/term v0.38.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace tarish => ../

replace github.com/missuo/opensnell => ../third_party/opensnell
