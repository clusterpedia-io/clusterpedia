//+build tools

package hack

import (
	_ "k8s.io/code-generator"
	_ "k8s.io/kubernetes/cmd/preferredimports"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
