module github.com/sbezverk/rt-collection

go 1.15

require (
	github.com/Shopify/sarama v1.27.2
	github.com/arangodb/go-driver v0.0.0-20201202080739-c41c94f2de00
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/sbezverk/gobmp v0.0.1-beta.0.20201206165019-4881ac927333
	github.com/sbezverk/topology v0.0.0-00010101000000-000000000000
)

replace (
	github.com/sbezverk/gobmp => ../gobmp
	github.com/sbezverk/topology => ../topology
)
