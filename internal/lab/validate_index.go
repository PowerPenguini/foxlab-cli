package lab

type validationIndex struct {
	switchIDs       map[string]struct{}
	externalLinkIDs map[string]struct{}
	vmIDs           map[string]struct{}
	containerIDs    map[string]struct{}
	vmNames         map[string]string
	containerNames  map[string]string
}
