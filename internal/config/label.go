package config

import (
	"fmt"
	"strings"
)

// ShortName returns the last path segment of an ARN or name.
func ShortName(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// TypeLabel is the human name of a target type.
func TypeLabel(typ string) string {
	switch typ {
	case TypeEKS:
		return "EKS"
	case TypeECS:
		return "ECS"
	}
	return typ
}

// Primary is the leaf identifier of a target (its service).
func (t Target) Primary() string {
	if t.Type == TypeECS {
		if s := ShortName(t.Service); s != "" {
			return s
		}
		return ShortName(t.Cluster)
	}
	if t.Service != "" {
		return t.Service
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Cluster
}

// Secondary is the parent hierarchy of a target.
func (t Target) Secondary(region string) string {
	var where string
	switch t.Type {
	case TypeEKS:
		where = fmt.Sprintf("%s / %s", t.Cluster, t.Namespace)
	case TypeECS:
		where = ShortName(t.Cluster)
		if t.LogGroup != "" {
			where += " (" + t.LogGroup + ")"
		}
	default:
		where = "unsupported target type"
	}
	return fmt.Sprintf("%s · %s [%s]", TypeLabel(t.Type), where, region)
}

// Label is the plain one-line description of a target.
func (t Target) Label(region string) string {
	return t.Primary() + " — " + t.Secondary(region)
}
