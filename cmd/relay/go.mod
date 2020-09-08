module git.sr.ht/~whereswaldon/sprout-go/cmd/relay

go 1.15

require (
	git.sr.ht/~whereswaldon/forest-go v0.0.0-20200908023146-ee23841ca265
	git.sr.ht/~whereswaldon/sprout-go v0.0.0-20200908023616-4e6573e18230
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/common v0.13.0 // indirect
	golang.org/x/sys v0.0.0-20200905004654-be1d3432aa8f // indirect
	google.golang.org/protobuf v1.25.0 // indirect
)

replace git.sr.ht/~whereswaldon/sprout-go => ../../

replace golang.org/x/crypto => github.com/ProtonMail/crypto v0.0.0-20200416114516-1fa7f403fb9c
