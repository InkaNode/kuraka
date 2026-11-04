package stalwart

import (
	"bytes"
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Account kinds, from the wire "@type".
const (
	KindUser  = "User"
	KindGroup = "Group"
)

// Domain is a Stalwart domain. Aliases are alias domains.
type Domain struct {
	ID          string
	Name        string
	Aliases     []string
	Enable      bool
	Description string
	ZoneFile    string
}

// Account is a mailbox (User) or shared mailbox (Group).
type Account struct {
	ID          string
	Kind        string
	Name        string
	DomainID    string
	Email       string
	Description string
	Aliases     []Alias
	QuotaBytes  uint64
	UsedBytes   uint64
}

// Alias is an extra address delivered to an account.
type Alias struct {
	Name        string
	DomainID    string
	Description string
	Enable      bool
}

// DkimKey is a DKIM signature key of a domain.
type DkimKey struct {
	ID        string
	DomainID  string
	Selector  string
	Algorithm string
	PublicKey string
	Stage     string
}

// NewAccount describes an account to create. Kind defaults to User.
type NewAccount struct {
	Kind        string
	Name        string
	DomainID    string
	Description string
	Password    string
	QuotaBytes  uint64
	Aliases     []Alias
}

// AccountPatch lists the fields to change; nil fields are left alone.
// QuotaBytes of 0 removes the quota. Aliases replaces the full list.
type AccountPatch struct {
	Description *string
	Password    *string
	QuotaBytes  *uint64
	Aliases     *[]Alias
}

// list is a JSON list encoded by Stalwart as {"0": ..., "1": ...}.
type list[T any] []T

func (l list[T]) MarshalJSON() ([]byte, error) {
	m := make(map[string]T, len(l))
	for i, v := range l {
		m[strconv.Itoa(i)] = v
	}
	return json.Marshal(m)
}

func (l *list[T]) UnmarshalJSON(b []byte) error {
	if b = bytes.TrimSpace(b); len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, (*[]T)(l))
	}
	var m map[string]T
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	// Shorter first orders decimal keys numerically ("2" before "10").
	keys := slices.SortedFunc(maps.Keys(m), func(a, b string) int {
		return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
	})
	*l = make(list[T], 0, len(keys))
	for _, k := range keys {
		*l = append(*l, m[k])
	}
	return nil
}

// stringSet is a set of names encoded by Stalwart as {"name": true}.
type stringSet []string

func (s stringSet) MarshalJSON() ([]byte, error) {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return json.Marshal(m)
}

func (s *stringSet) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*s = nil
		return nil
	case len(b) > 0 && b[0] == '[':
		var v []string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = v
		return nil
	}
	var m map[string]bool
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	out := make([]string, 0, len(m))
	for k, ok := range m {
		if ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	*s = out
	return nil
}

// typeName decodes either a plain string or a polymorphic {"@type": ...}.
type typeName string

func (t *typeName) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = typeName(s)
		return nil
	}
	var o struct {
		Type string `json:"@type"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*t = typeName(o.Type)
	return nil
}

type wireDomain struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Aliases     stringSet `json:"aliases"`
	Enable      bool      `json:"enable"`
	Description string    `json:"description"`
	DNSZoneFile string    `json:"dnsZoneFile"`
}

func (w wireDomain) domain() Domain {
	return Domain{
		ID:          w.ID,
		Name:        w.Name,
		Aliases:     []string(w.Aliases),
		Enable:      w.Enable,
		Description: w.Description,
		ZoneFile:    w.DNSZoneFile,
	}
}

type wireAlias struct {
	Name        string  `json:"name"`
	DomainID    string  `json:"domainId"`
	Description *string `json:"description"`
	Enable      bool    `json:"enable"`
}

func toWireAliases(as []Alias) list[wireAlias] {
	out := make(list[wireAlias], 0, len(as))
	for _, a := range as {
		w := wireAlias{Name: a.Name, DomainID: a.DomainID, Enable: a.Enable}
		if a.Description != "" {
			w.Description = &a.Description
		}
		out = append(out, w)
	}
	return out
}

type wireAccount struct {
	ID           string          `json:"id"`
	Type         string          `json:"@type"`
	Name         string          `json:"name"`
	DomainID     string          `json:"domainId"`
	EmailAddress string          `json:"emailAddress"`
	Description  string          `json:"description"`
	Aliases      list[wireAlias] `json:"aliases"`
	Quotas       struct {
		MaxDiskQuota  uint64 `json:"maxDiskQuota"`
		UsedDiskQuota uint64 `json:"usedDiskQuota"`
	} `json:"quotas"`
	UsedDiskQuota uint64 `json:"usedDiskQuota"`
}

func (w wireAccount) account() Account {
	a := Account{
		ID:          w.ID,
		Kind:        w.Type,
		Name:        w.Name,
		DomainID:    w.DomainID,
		Email:       w.EmailAddress,
		Description: w.Description,
		QuotaBytes:  w.Quotas.MaxDiskQuota,
		UsedBytes:   w.UsedDiskQuota,
	}
	if a.UsedBytes == 0 {
		a.UsedBytes = w.Quotas.UsedDiskQuota
	}
	for _, al := range w.Aliases {
		x := Alias{Name: al.Name, DomainID: al.DomainID, Enable: al.Enable}
		if al.Description != nil {
			x.Description = *al.Description
		}
		a.Aliases = append(a.Aliases, x)
	}
	return a
}

type wireDkim struct {
	ID        string   `json:"id"`
	DomainID  string   `json:"domainId"`
	Selector  string   `json:"selector"`
	Type      string   `json:"@type"`
	PublicKey string   `json:"publicKey"`
	Stage     typeName `json:"stage"`
}

func (w wireDkim) key() DkimKey {
	return DkimKey{
		ID:        w.ID,
		DomainID:  w.DomainID,
		Selector:  w.Selector,
		Algorithm: w.Type,
		PublicKey: w.PublicKey,
		Stage:     string(w.Stage),
	}
}
