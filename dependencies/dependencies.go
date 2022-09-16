package dependencies

// Dependencies is an interface that can be implemented to inject custom
// behaviour.
type Dependencies interface {
	Disrupt(s string) bool
}

// ProdDependencies are the default dependencies which don't change the
// behaviour at all.
var ProdDependencies Dependencies = &prodDependencies{}

// prodDependencies is a struct that implements Disrupt as a no-op.
type prodDependencies struct {
}

// Disrupt satisfies the Dependencies interface. It always returns 'false'.
func (p *prodDependencies) Disrupt(_ string) bool {
	return false
}
