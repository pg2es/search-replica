// package conftags implements Go struct tag like syntax parser.
// Used exclusively in Search Replica. You probably don't need it.
// Syntaxt is slightly different from standard Go struct tag syntax.
// Contains code from golang standard library (reflect package)
package conftags

import (
	"errors"
	"strconv"
	"strings"
)

// A Tag is the tag string in a config.
//
// By convention, tag strings are a concatenation of
// optionally space-separated key:"value" pairs.
// Each key is a non-empty string consisting of non-control
// characters other than space (U+0020 ' '), quote (U+0022 '"'),
// and colon (U+003A ':').  Each value is quoted using U+0022 '"'
// characters and Go string literal syntax.
type Tag struct {
	Name   string
	Values []string
}

// Tags can have multiple tags with same name.
type Tags []*Tag

func (tags *Tags) append(tag *Tag) {
	*tags = append(*tags, tag)
}

func (tags Tags) Filter(name string) (result Tags) {
	for _, tag := range tags {
		if tag.Name == name {
			result = append(result, tag)
		}
	}
	return result
}

// returns tag if found, otherwise returns nil
func (tags Tags) Get(name string) *Tag {
	for _, tag := range tags {
		if tag.Name == name {
			return tag
		}
	}
	return nil
}

var errSyntax = errors.New("syntax error")

// Parse is modified reflect.StructTag.Lookup. Check original method for explanation
// Changes:
// - all tags are parsed and stored in list of tags (even duplicates);
// - if there are 3 or more spaces in between tags, rest of a string is considered as comment;
// - now it returns syntax error

func (tags *Tags) Parse(src string) error {
	for src != "" {
		// Skip leading space.
		i := 0
		for i < len(src) && src[i] == ' ' {
			i++
		}
		src = src[i:]
		if src == "" {
			return nil // empty tags are OK
		}
		if i > 3 || src[0] == '#' {
			return nil // skip comment part
		}

		i = 0
		for i < len(src) && src[i] > ' ' && src[i] != ':' && src[i] != '"' && src[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(src) || src[i] != ':' || src[i+1] != '"' {
			return errSyntax
		}
		name := string(src[:i])
		src = src[i+1:]

		// Scan quoted string to find value.
		i = 1
		for i < len(src) && src[i] != '"' {
			if src[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(src) {
			return errSyntax
		}
		qvalue := string(src[:i+1])
		src = src[i+1:]

		values, err := strconv.Unquote(qvalue)
		if err != nil {
			return errSyntax
		}
		tags.append(&Tag{
			Name:   name,
			Values: strings.Split(values, ","),
		})
	}
	return nil
}

func Parse(src string) (tags Tags, err error) {
	err = tags.Parse(src)
	return tags, err
}
