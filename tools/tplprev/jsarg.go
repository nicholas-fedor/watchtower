package main

type jsValueKind int

const (
	jsKindInvalid jsValueKind = iota
	jsKindString
	jsKindCollection
)

// classifyJSType classifies a JavaScript type name from js.Type.String().
//
// Strings use the compact character format. Objects cover arrays and typed
// arrays. Other types are rejected so Length is never called on them.
//
// Parameters:
//   - typeName: JavaScript type name (string, object, null, undefined, ...).
//
// Returns:
//   - jsValueKind: String, collection, or invalid.
func classifyJSType(typeName string) jsValueKind {
	switch typeName {
	case "string":
		return jsKindString
	case "object":
		return jsKindCollection
	default:
		return jsKindInvalid
	}
}
