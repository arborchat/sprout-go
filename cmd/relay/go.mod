module git.sr.ht/~whereswaldon/sprout-go/cmd/relay

go 1.15

require (
	git.sr.ht/~whereswaldon/forest-go v0.0.0-20200712155735-6acac05fe174
	git.sr.ht/~whereswaldon/sprout-go v0.0.0-20200517010141-a4188845a9a8
	github.com/prometheus/client_golang v1.7.1
)

replace git.sr.ht/~whereswaldon/sprout-go => ../../

replace golang.org/x/crypto => github.com/ProtonMail/crypto v0.0.0-20200416114516-1fa7f403fb9c
