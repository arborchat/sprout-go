module git.sr.ht/~whereswaldon/sprout-go/cmd/relay

go 1.15

require (
	git.sr.ht/~athorp96/forest-ex v0.0.0-20201012012825-01012995abe1
	git.sr.ht/~whereswaldon/forest-go v0.0.0-20201031211815-0e51e2b5d99c
	git.sr.ht/~whereswaldon/sprout-go v0.0.0-20201012005450-884e34166a53
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/prometheus/client_golang v1.8.0
	golang.org/x/sys v0.0.0-20201029080932-201ba4db2418 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
)

replace git.sr.ht/~whereswaldon/sprout-go => ../../

replace golang.org/x/crypto => github.com/ProtonMail/crypto v0.0.0-20201022141144-3fe6b6992c0f
