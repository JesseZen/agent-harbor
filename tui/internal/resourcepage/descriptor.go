package resourcepage

import "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"

type FieldKind = generated.FieldKind
type FieldDescriptor = generated.FieldDescriptor
type ResourceDescriptor = generated.ResourceDescriptor
type ResourceKind = generated.ResourceKind

// Lookup returns the generated descriptor for a resource kind.
func Lookup(kind ResourceKind) (ResourceDescriptor, bool) {
	d, ok := generated.Descriptors[kind]
	return d, ok
}

// AllDescriptors returns a stable copy of every generated descriptor.
func AllDescriptors() map[ResourceKind]ResourceDescriptor {
	out := make(map[ResourceKind]ResourceDescriptor, len(generated.Descriptors))
	for kind, descriptor := range generated.Descriptors {
		out[kind] = descriptor
	}
	return out
}
