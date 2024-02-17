package internalstorage

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// AllowRawSQLQuery is a feature gate for the apiserver to allow querying by the raw sql.
	//
	// owner: @cleverhu
	// alpha: v0.3.0
	AllowRawSQLQuery featuregate.Feature = "AllowRawSQLQuery"

	// AllowParameterizedSQLQuery is a feature gate for the apiserver to allow querying by the parameterized SQL
	// for better defense against SQL injection.
	//
	// Use either single whereSQLStatement field, a pair of whereSQLStatement with whereSQLParam, or
	// whereSQLStatement with whereSQLJSONParams to pass the SQL itself and parameters.
	//
	// owner: @nekomeowww
	// alpha: v0.8.0
	AllowParameterizedSQLQuery featuregate.Feature = "AllowParameterizedSQLQuery"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultInternalStorageFeatureGates))
}

// defaultInternalStorageFeatureGates consists of all known custom internalstorage feature keys.
// To add a new feature, define a key for it above and add it here.
var defaultInternalStorageFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	AllowRawSQLQuery:           {Default: false, PreRelease: featuregate.Alpha},
	AllowParameterizedSQLQuery: {Default: false, PreRelease: featuregate.Alpha},
}
