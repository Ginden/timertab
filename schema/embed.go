package schema

import _ "embed"

const V1URL = "https://raw.githubusercontent.com/ginden/timertab/v1.1.0/schema/v1.json"

// V1JSON is the embedded v1 schema used for runtime validation in release binaries.
//
//go:embed v1.json
var V1JSON []byte
