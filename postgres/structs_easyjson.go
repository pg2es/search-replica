// Code generated by easyjson for marshaling/unmarshaling. DO NOT EDIT.

package postgres

import (
	json "encoding/json"
	easyjson "github.com/mailru/easyjson"
	jlexer "github.com/mailru/easyjson/jlexer"
	jwriter "github.com/mailru/easyjson/jwriter"
)

// suppress unused package warning
var (
	_ *json.RawMessage
	_ *jlexer.Lexer
	_ *jwriter.Writer
	_ easyjson.Marshaler
)

func easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres(in *jlexer.Lexer, out *documentJoin) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "name":
			out.Name = string(in.String())
		case "parent":
			out.Parent = string(in.String())
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}
func easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres(out *jwriter.Writer, in documentJoin) {
	out.RawByte('{')
	first := true
	_ = first
	{
		const prefix string = ",\"name\":"
		out.RawString(prefix[1:])
		out.String(string(in.Name))
	}
	if in.Parent != "" {
		const prefix string = ",\"parent\":"
		out.RawString(prefix)
		out.String(string(in.Parent))
	}
	out.RawByte('}')
}

// MarshalJSON supports json.Marshaler interface
func (v documentJoin) MarshalJSON() ([]byte, error) {
	w := jwriter.Writer{}
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres(&w, v)
	return w.Buffer.BuildBytes(), w.Error
}

// MarshalEasyJSON supports easyjson.Marshaler interface
func (v documentJoin) MarshalEasyJSON(w *jwriter.Writer) {
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres(w, v)
}

// UnmarshalJSON supports json.Unmarshaler interface
func (v *documentJoin) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres(&r, v)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (v *documentJoin) UnmarshalEasyJSON(l *jlexer.Lexer) {
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres(l, v)
}
func easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres1(in *jlexer.Lexer, out *bulkHeaderPayload) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "_index":
			out.Index = string(in.String())
		case "_id":
			out.ID = string(in.String())
		case "routing":
			out.Routing = string(in.String())
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}
func easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres1(out *jwriter.Writer, in bulkHeaderPayload) {
	out.RawByte('{')
	first := true
	_ = first
	{
		const prefix string = ",\"_index\":"
		out.RawString(prefix[1:])
		out.String(string(in.Index))
	}
	if in.ID != "" {
		const prefix string = ",\"_id\":"
		out.RawString(prefix)
		out.String(string(in.ID))
	}
	if in.Routing != "" {
		const prefix string = ",\"routing\":"
		out.RawString(prefix)
		out.String(string(in.Routing))
	}
	out.RawByte('}')
}

// MarshalJSON supports json.Marshaler interface
func (v bulkHeaderPayload) MarshalJSON() ([]byte, error) {
	w := jwriter.Writer{}
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres1(&w, v)
	return w.Buffer.BuildBytes(), w.Error
}

// MarshalEasyJSON supports easyjson.Marshaler interface
func (v bulkHeaderPayload) MarshalEasyJSON(w *jwriter.Writer) {
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres1(w, v)
}

// UnmarshalJSON supports json.Unmarshaler interface
func (v *bulkHeaderPayload) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres1(&r, v)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (v *bulkHeaderPayload) UnmarshalEasyJSON(l *jlexer.Lexer) {
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres1(l, v)
}
func easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres2(in *jlexer.Lexer, out *bulkHeader) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "insert":
			if in.IsNull() {
				in.Skip()
				out.Insert = nil
			} else {
				if out.Insert == nil {
					out.Insert = new(bulkHeaderPayload)
				}
				(*out.Insert).UnmarshalEasyJSON(in)
			}
		case "update":
			if in.IsNull() {
				in.Skip()
				out.Update = nil
			} else {
				if out.Update == nil {
					out.Update = new(bulkHeaderPayload)
				}
				(*out.Update).UnmarshalEasyJSON(in)
			}
		case "delete":
			if in.IsNull() {
				in.Skip()
				out.Delete = nil
			} else {
				if out.Delete == nil {
					out.Delete = new(bulkHeaderPayload)
				}
				(*out.Delete).UnmarshalEasyJSON(in)
			}
		case "index":
			if in.IsNull() {
				in.Skip()
				out.Index = nil
			} else {
				if out.Index == nil {
					out.Index = new(bulkHeaderPayload)
				}
				(*out.Index).UnmarshalEasyJSON(in)
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}
func easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres2(out *jwriter.Writer, in bulkHeader) {
	out.RawByte('{')
	first := true
	_ = first
	if in.Insert != nil {
		const prefix string = ",\"insert\":"
		first = false
		out.RawString(prefix[1:])
		(*in.Insert).MarshalEasyJSON(out)
	}
	if in.Update != nil {
		const prefix string = ",\"update\":"
		if first {
			first = false
			out.RawString(prefix[1:])
		} else {
			out.RawString(prefix)
		}
		(*in.Update).MarshalEasyJSON(out)
	}
	if in.Delete != nil {
		const prefix string = ",\"delete\":"
		if first {
			first = false
			out.RawString(prefix[1:])
		} else {
			out.RawString(prefix)
		}
		(*in.Delete).MarshalEasyJSON(out)
	}
	if in.Index != nil {
		const prefix string = ",\"index\":"
		if first {
			first = false
			out.RawString(prefix[1:])
		} else {
			out.RawString(prefix)
		}
		(*in.Index).MarshalEasyJSON(out)
	}
	out.RawByte('}')
}

// MarshalJSON supports json.Marshaler interface
func (v bulkHeader) MarshalJSON() ([]byte, error) {
	w := jwriter.Writer{}
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres2(&w, v)
	return w.Buffer.BuildBytes(), w.Error
}

// MarshalEasyJSON supports easyjson.Marshaler interface
func (v bulkHeader) MarshalEasyJSON(w *jwriter.Writer) {
	easyjson6a975c40EncodeGithubComHatchStudioSearchReplicaPostgres2(w, v)
}

// UnmarshalJSON supports json.Unmarshaler interface
func (v *bulkHeader) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres2(&r, v)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (v *bulkHeader) UnmarshalEasyJSON(l *jlexer.Lexer) {
	easyjson6a975c40DecodeGithubComHatchStudioSearchReplicaPostgres2(l, v)
}
