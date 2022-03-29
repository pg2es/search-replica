package conftags

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	type args struct {
	}
	tests := []struct {
		name     string
		src      string
		wantErr  bool
		wantTags Tags
	}{
		{"empty", "", false, nil},
		{"invalid syntax", "currency is ISO4217 code", true, nil},
		{"simple", `tag:"value"`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
		}},
		{"multiple", `tag:"value" tag2:"VALUE2"`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
			&Tag{Name: "tag2", Values: []string{"VALUE2"}},
		}},
		{"multiple no space", `tag:"value"tag2:"VALUE2"`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
			&Tag{Name: "tag2", Values: []string{"VALUE2"}},
		}},
		{"multiple with same name", `tag:"val1"tag:"val2" tag:"val3"`, false, Tags{
			&Tag{Name: "tag", Values: []string{"val1"}},
			&Tag{Name: "tag", Values: []string{"val2"}},
			&Tag{Name: "tag", Values: []string{"val3"}},
		}},
		{"multiple with space comment", `tag:"value" tag2:"VALUE2"    Some human readable part of comment`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
			&Tag{Name: "tag2", Values: []string{"VALUE2"}},
		}},
		{"multiple no space with comment", `tag:"value"tag2:"VALUE2"#HumanReadablePartOfComment`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
			&Tag{Name: "tag2", Values: []string{"VALUE2"}},
		}},
		{"multiple no space with comment tag", `tag:"value"tag2:"VALUE2"#tag3:"value3"`, false, Tags{
			&Tag{Name: "tag", Values: []string{"value"}},
			&Tag{Name: "tag2", Values: []string{"VALUE2"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTags, err := Parse(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotTags, tt.wantTags) {
				t.Errorf("Parse() = %v, want %v", gotTags, tt.wantTags)
			}
		})
	}
}
