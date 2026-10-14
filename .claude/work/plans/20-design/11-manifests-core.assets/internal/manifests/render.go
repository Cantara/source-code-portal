package manifests

import "github.com/cantara/source-code-portal/internal/manifests/internal/purl"

// PURLFormatter renders the purl of one component of an ecosystem whose purl rules differ from the default (RR-1 of unit 13).
// display is Dependency.TargetDisplay, qualifiers the canonical Dependency.Qualifiers string (reserved key "subpath" is the purl subpath).
// ok=false means "this component has no purl" (the API then omits it and CycloneDX skips the component).
type PURLFormatter func(id ComponentID, display, version, qualifiers string) (purl string, ok bool)

func renderPURL(c ComponentID, version string) string {
	s, err := purl.Build(string(c.Ecosystem), c.Namespace, c.Name, version, "")
	if err != nil {
		return ""
	}
	return s
}

// FormatPURL renders the purl of a component with the ecosystem's formatter (Registry.RegisterEcosystem), or the default formatter.
// It is the only place purls are produced for the API, the markdown and CycloneDX.
func (r *Registry) FormatPURL(id ComponentID, display, version, qualifiers string) (string, bool) {
	if sp, ok := r.specs[id.Ecosystem]; ok && sp.FormatPURL != nil {
		return sp.FormatPURL(id, display, version, qualifiers)
	}
	typ := purl.Type(string(id.Ecosystem))
	if typ == "" {
		typ = string(id.Ecosystem)
	}
	q, sub := purl.ParseQualifiers(qualifiers)
	s := purl.Format(typ, id.Namespace, id.Name, version, q, sub)
	return s, s != ""
}
