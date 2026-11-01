package deployaccess

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// Document is an IAM policy document.
type Document struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement is one IAM policy statement. Condition values are single strings,
// which is all Sema's policies need.
type Statement struct {
	Sid       string                       `json:"Sid,omitempty"`
	Effect    string                       `json:"Effect"`
	Principal map[string]string            `json:"Principal,omitempty"`
	Action    []string                     `json:"Action"`
	Resource  []string                     `json:"Resource,omitempty"`
	Condition map[string]map[string]string `json:"Condition,omitempty"`
}

// JSON encodes the document for an IAM API.
func (d Document) JSON() (string, error) {
	encoded, err := json.Marshal(d)
	return string(encoded), err
}

// Request is one call to evaluate against a document. An empty Resource or
// Principal matches statements that leave that element unset.
type Request struct {
	Action    string
	Resource  string
	Principal string
	Context   map[string]string
}

// Allows reports whether the document alone allows the request: some Allow
// statement matches and no Deny statement does. It supports the subset of IAM
// Sema's policies use: Action and Resource wildcards, principal values, and the
// StringEquals condition operator. It panics on any other operator, so a policy
// can't silently outgrow its tests.
func (d Document) Allows(request Request) bool {
	allowed := false
	for _, statement := range d.Statement {
		if !statement.matches(request) {
			continue
		}
		if statement.Effect == "Deny" {
			return false
		}
		if statement.Effect == "Allow" {
			allowed = true
		}
	}
	return allowed
}

func (s Statement) matches(request Request) bool {
	if !matchesAny(s.Action, request.Action, true) {
		return false
	}
	if len(s.Resource) > 0 && !matchesAny(s.Resource, request.Resource, false) {
		return false
	}
	if len(s.Principal) > 0 && !slices.Contains(slices.Collect(maps.Values(s.Principal)), request.Principal) {
		return false
	}
	for operator, entries := range s.Condition {
		if operator != "StringEquals" {
			panic("deployaccess: unsupported condition operator " + operator)
		}
		for key, want := range entries {
			if got, ok := request.Context[key]; !ok || got != want {
				return false
			}
		}
	}
	return true
}

func matchesAny(patterns []string, value string, foldCase bool) bool {
	for _, pattern := range patterns {
		if glob(pattern, value, foldCase) {
			return true
		}
	}
	return false
}

// glob matches IAM wildcards: * matches any run of characters, ? any one.
func glob(pattern, value string, foldCase bool) bool {
	if foldCase {
		pattern, value = strings.ToLower(pattern), strings.ToLower(value)
	}
	p, v := 0, 0
	star, mark := -1, 0
	for v < len(value) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == value[v]):
			p++
			v++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, v
			p++
		case star >= 0:
			p = star + 1
			mark++
			v = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
