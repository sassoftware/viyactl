package configtypes

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/printer"
	"github.com/goccy/go-yaml/token"

	"github.com/goccy/go-yaml"
)

/*
This only exists because I could not get various parts of configs to not parse to scientific notation
This file should be cleaned up *a lot*

https://github.com/goccy/go-yaml/blob/92bc79cb5f685e999ad131473168fc45215d12d9/encode.go
*/

const (
	// DefaultIndentSpaces default number of space for indent
	DefaultIndentSpaces = 2
	lowerhex            = "0123456789abcdef"
)

var (
	globalCustomMarshalerMu  sync.Mutex
	globalCustomMarshalerMap = map[reflect.Type]func(context.Context, any) ([]byte, error){}
)

func quoteWith(s string, quote byte) string {
	return string(appendQuotedWith(make([]byte, 0, 3*len(s)/2), s, quote))
}

func appendQuotedWith(buf []byte, s string, quote byte) []byte {
	// Often called with big strings, so preallocate. If there's quoting,
	// this is conservative but still helps a lot.
	if cap(buf)-len(buf) < len(s) {
		nBuf := make([]byte, len(buf), len(buf)+1+len(s)+1)
		copy(nBuf, buf)
		buf = nBuf
	}
	buf = append(buf, quote)
	for width := 0; len(s) > 0; s = s[width:] {
		r := rune(s[0])
		width = 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(s)
		}
		if width == 1 && r == utf8.RuneError {
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[s[0]>>4])
			buf = append(buf, lowerhex[s[0]&0xF])
			continue
		}
		buf = appendEscapedRune(buf, r, quote)
	}
	buf = append(buf, quote)
	return buf
}

func appendEscapedRune(buf []byte, r rune, quote byte) []byte {
	var runeTmp [utf8.UTFMax]byte
	// goccy/go-yaml patch on top of the standard library's appendEscapedRune function.
	//
	// We use this to implement the YAML single-quoted string, where the only escape sequence is '', which represents a single quote.
	// The below snippet from the standard library is for escaping e.g. \ with \\, which is not what we want for the single-quoted string.
	//
	// if r == rune(quote) || r == '\\' { // always backslashed
	// 	buf = append(buf, '\\')
	// 	buf = append(buf, byte(r))
	// 	return buf
	// }
	if r == rune(quote) {
		buf = append(buf, byte(r))
		buf = append(buf, byte(r))
		return buf
	}
	if unicode.IsPrint(r) {
		n := utf8.EncodeRune(runeTmp[:], r)
		buf = append(buf, runeTmp[:n]...)
		return buf
	}
	switch r {
	case '\a':
		buf = append(buf, `\a`...)
	case '\b':
		buf = append(buf, `\b`...)
	case '\f':
		buf = append(buf, `\f`...)
	case '\n':
		buf = append(buf, `\n`...)
	case '\r':
		buf = append(buf, `\r`...)
	case '\t':
		buf = append(buf, `\t`...)
	case '\v':
		buf = append(buf, `\v`...)
	default:
		switch {
		case r < ' ':
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[byte(r)>>4])
			buf = append(buf, lowerhex[byte(r)&0xF])
		case r > utf8.MaxRune:
			r = 0xFFFD
			fallthrough
		case r < 0x10000:
			buf = append(buf, `\u`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		default:
			buf = append(buf, `\U`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}

// ConfigEncoder writes YAML values to an output stream.
type ConfigEncoder struct {
	writer                     io.Writer
	opts                       []yaml.EncodeOption
	singleQuote                bool
	isFlowStyle                bool
	isJSONStyle                bool
	useJSONMarshaler           bool
	enableSmartAnchor          bool
	aliasRefToName             map[uintptr]string
	anchorRefToName            map[uintptr]string
	anchorNameMap              map[string]struct{}
	anchorCallback             func(*ast.AnchorNode, any) error
	customMarshalerMap         map[reflect.Type]func(context.Context, any) ([]byte, error)
	omitZero                   bool
	omitEmpty                  bool
	autoInt                    bool
	useLiteralStyleIfMultiline bool
	commentMap                 map[*yaml.Path][]*yaml.Comment
	written                    bool

	line           int
	column         int
	offset         int
	indentNum      int
	indentLevel    int
	indentSequence bool
}

// NewEncoder returns a new encoder that writes to w.
// The Encoder should be closed after use to flush all data to w.
func NewEncoder(w io.Writer, opts ...yaml.EncodeOption) *ConfigEncoder {
	return &ConfigEncoder{
		writer:             w,
		opts:               opts,
		customMarshalerMap: map[reflect.Type]func(context.Context, any) ([]byte, error){},
		line:               1,
		column:             1,
		offset:             0,
		indentNum:          DefaultIndentSpaces,
		anchorRefToName:    make(map[uintptr]string),
		anchorNameMap:      make(map[string]struct{}),
		aliasRefToName:     make(map[uintptr]string),
	}
}

// Close closes the encoder by writing any remaining data.
// It does not write a stream terminating string "...".
func (e *ConfigEncoder) Close() error {
	return nil
}

// Encode writes the YAML encoding of v to the stream.
// If multiple items are encoded to the stream,
// the second and subsequent document will be preceded with a "---" document separator,
// but the first will not.
//
// See the documentation for Marshal for details about the conversion of Go values to YAML.
func (e *ConfigEncoder) Encode(v any) error {
	return e.EncodeContext(context.Background(), v)
}

// EncodeContext writes the YAML encoding of v to the stream with context.Context.
func (e *ConfigEncoder) EncodeContext(ctx context.Context, v any) error {
	node, err := e.EncodeToNodeContext(ctx, v)
	if err != nil {
		return err
	}
	if err := e.setCommentByCommentMap(node); err != nil {
		return err
	}
	if !e.written {
		e.written = true
	} else {
		// write document separator
		_, _ = e.writer.Write([]byte("---\n"))
	}
	var p printer.Printer
	_, _ = e.writer.Write(p.PrintNode(node))
	return nil
}

// EncodeToNode convert v to ast.Node.
func (e *ConfigEncoder) EncodeToNode(v any) (ast.Node, error) {
	return e.EncodeToNodeContext(context.Background(), v)
}

// EncodeToNodeContext convert v to ast.Node with context.Context.
func (e *ConfigEncoder) EncodeToNodeContext(ctx context.Context, v any) (ast.Node, error) {
	// for _, opt := range e.opts {
	// 	if err := opt(e); err != nil {
	// 		return nil, err
	// 	}
	// }
	if e.enableSmartAnchor {
		// during the first encoding, store all mappings between alias addresses and their names.
		if _, err := e.encodeValue(ctx, reflect.ValueOf(v), 1); err != nil {
			return nil, err
		}
		e.clearSmartAnchorRef()
	}
	node, err := e.encodeValue(ctx, reflect.ValueOf(v), 1)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (e *ConfigEncoder) setCommentByCommentMap(node ast.Node) error {
	if e.commentMap == nil {
		return nil
	}
	for path, comments := range e.commentMap {
		n, err := path.FilterNode(node)
		if err != nil {
			return err
		}
		if n == nil {
			continue
		}
		for _, comment := range comments {
			commentTokens := []*token.Token{}
			for _, text := range comment.Texts {
				commentTokens = append(commentTokens, token.New(text, text, nil))
			}
			commentGroup := ast.CommentGroup(commentTokens)
			switch comment.Position {
			case yaml.CommentHeadPosition:
				if err := e.setHeadComment(node, n, commentGroup); err != nil {
					return err
				}
			case yaml.CommentLinePosition:
				if err := e.setLineComment(node, n, commentGroup); err != nil {
					return err
				}
			case yaml.CommentFootPosition:
				if err := e.setFootComment(node, n, commentGroup); err != nil {
					return err
				}
			default:
				return yaml.ErrUnknownCommentPositionType
			}
		}
	}
	return nil
}

func (e *ConfigEncoder) setHeadComment(node, filtered ast.Node, comment *ast.CommentGroupNode) error {
	parent := ast.Parent(node, filtered)
	if parent == nil {
		return yaml.ErrUnsupportedHeadPositionType(node)
	}
	switch p := parent.(type) {
	case *ast.MappingValueNode:
		if err := p.SetComment(comment); err != nil {
			return err
		}
	case *ast.MappingNode:
		if err := p.SetComment(comment); err != nil {
			return err
		}
	case *ast.SequenceNode:
		if len(p.ValueHeadComments) == 0 {
			p.ValueHeadComments = make([]*ast.CommentGroupNode, len(p.Values))
		}
		var foundIdx int
		for idx, v := range p.Values {
			if v == filtered {
				foundIdx = idx
				break
			}
		}
		p.ValueHeadComments[foundIdx] = comment
	default:
		return yaml.ErrUnsupportedHeadPositionType(node)
	}
	return nil
}

func (e *ConfigEncoder) setLineComment(node, filtered ast.Node, comment *ast.CommentGroupNode) error {
	switch filtered.(type) {
	case *ast.MappingValueNode, *ast.SequenceNode:
		// Line comment cannot be set for mapping value node.
		// It should probably be set for the parent map node
		if err := e.setLineCommentToParentMapNode(node, filtered, comment); err != nil {
			return err
		}
	default:
		if err := filtered.SetComment(comment); err != nil {
			return err
		}
	}
	return nil
}

func (e *ConfigEncoder) setLineCommentToParentMapNode(node, filtered ast.Node, comment *ast.CommentGroupNode) error {
	parent := ast.Parent(node, filtered)
	if parent == nil {
		return yaml.ErrUnsupportedLinePositionType(node)
	}
	switch p := parent.(type) {
	case *ast.MappingValueNode:
		if err := p.Key.SetComment(comment); err != nil {
			return err
		}
	case *ast.MappingNode:
		if err := p.SetComment(comment); err != nil {
			return err
		}
	default:
		return yaml.ErrUnsupportedLinePositionType(parent)
	}
	return nil
}

func (e *ConfigEncoder) setFootComment(node, filtered ast.Node, comment *ast.CommentGroupNode) error {
	parent := ast.Parent(node, filtered)
	if parent == nil {
		return yaml.ErrUnsupportedFootPositionType(node)
	}
	switch n := parent.(type) {
	case *ast.MappingValueNode:
		n.FootComment = comment
	case *ast.MappingNode:
		n.FootComment = comment
	case *ast.SequenceNode:
		n.FootComment = comment
	default:
		return yaml.ErrUnsupportedFootPositionType(n)
	}
	return nil
}

func (e *ConfigEncoder) encodeDocument(doc []byte) (ast.Node, error) {
	f, err := parser.ParseBytes(doc, 0)
	if err != nil {
		return nil, err
	}
	for _, docNode := range f.Docs {
		if docNode.Body != nil {
			return docNode.Body, nil
		}
	}
	return nil, nil
}

func (e *ConfigEncoder) isInvalidValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	kind := v.Type().Kind()
	if kind == reflect.Ptr && v.IsNil() {
		return true
	}
	if kind == reflect.Interface && v.IsNil() {
		return true
	}
	return false
}

type jsonMarshaler interface {
	MarshalJSON() ([]byte, error)
}

func (e *ConfigEncoder) existsTypeInCustomMarshalerMap(t reflect.Type) bool {
	if _, exists := e.customMarshalerMap[t]; exists {
		return true
	}

	globalCustomMarshalerMu.Lock()
	defer globalCustomMarshalerMu.Unlock()
	if _, exists := globalCustomMarshalerMap[t]; exists {
		return true
	}
	return false
}

func (e *ConfigEncoder) marshalerFromCustomMarshalerMap(t reflect.Type) (func(context.Context, any) ([]byte, error), bool) {
	if marshaler, exists := e.customMarshalerMap[t]; exists {
		return marshaler, exists
	}

	globalCustomMarshalerMu.Lock()
	defer globalCustomMarshalerMu.Unlock()
	if marshaler, exists := globalCustomMarshalerMap[t]; exists {
		return marshaler, exists
	}
	return nil, false
}

func (e *ConfigEncoder) canEncodeByMarshaler(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}
	if e.existsTypeInCustomMarshalerMap(v.Type()) {
		return true
	}
	iface := v.Interface()
	switch iface.(type) {
	case yaml.BytesMarshalerContext:
		return true
	case yaml.BytesMarshaler:
		return true
	case yaml.InterfaceMarshalerContext:
		return true
	case yaml.InterfaceMarshaler:
		return true
	case time.Time, *time.Time:
		return true
	case time.Duration:
		return true
	case encoding.TextMarshaler:
		return true
	case jsonMarshaler:
		return e.useJSONMarshaler
	}
	return false
}

func (e *ConfigEncoder) encodeByMarshaler(ctx context.Context, v reflect.Value, column int) (ast.Node, error) {
	iface := v.Interface()

	if marshaler, exists := e.marshalerFromCustomMarshalerMap(v.Type()); exists {
		doc, err := marshaler(ctx, iface)
		if err != nil {
			return nil, err
		}
		node, err := e.encodeDocument(doc)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	if marshaler, ok := iface.(yaml.BytesMarshalerContext); ok {
		doc, err := marshaler.MarshalYAML(ctx)
		if err != nil {
			return nil, err
		}
		node, err := e.encodeDocument(doc)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	if marshaler, ok := iface.(yaml.BytesMarshaler); ok {
		doc, err := marshaler.MarshalYAML()
		if err != nil {
			return nil, err
		}
		node, err := e.encodeDocument(doc)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	if marshaler, ok := iface.(yaml.InterfaceMarshalerContext); ok {
		marshalV, err := marshaler.MarshalYAML(ctx)
		if err != nil {
			return nil, err
		}
		return e.encodeValue(ctx, reflect.ValueOf(marshalV), column)
	}

	if marshaler, ok := iface.(yaml.InterfaceMarshaler); ok {
		marshalV, err := marshaler.MarshalYAML()
		if err != nil {
			return nil, err
		}
		return e.encodeValue(ctx, reflect.ValueOf(marshalV), column)
	}

	if t, ok := iface.(time.Time); ok {
		return e.encodeTime(t, column), nil
	}
	// Handle *time.Time explicitly since it implements TextMarshaler and shouldn't be treated as plain text
	if t, ok := iface.(*time.Time); ok && t != nil {
		return e.encodeTime(*t, column), nil
	}

	if t, ok := iface.(time.Duration); ok {
		return e.encodeDuration(t, column), nil
	}

	if marshaler, ok := iface.(encoding.TextMarshaler); ok {
		text, err := marshaler.MarshalText()
		if err != nil {
			return nil, err
		}
		node := e.encodeString(string(text), column)
		return node, nil
	}

	if e.useJSONMarshaler {
		if marshaler, ok := iface.(jsonMarshaler); ok {
			jsonBytes, err := marshaler.MarshalJSON()
			if err != nil {
				return nil, err
			}
			doc, err := yaml.JSONToYAML(jsonBytes)
			if err != nil {
				return nil, err
			}
			node, err := e.encodeDocument(doc)
			if err != nil {
				return nil, err
			}
			return node, nil
		}
	}

	return nil, errors.New("does not implemented Marshaler")
}

func (e *ConfigEncoder) encodeValue(ctx context.Context, v reflect.Value, column int) (ast.Node, error) {
	if e.isInvalidValue(v) {
		return e.encodeNil(), nil
	}
	if e.canEncodeByMarshaler(v) {
		node, err := e.encodeByMarshaler(ctx, v, column)
		if err != nil {
			return nil, err
		}
		return node, nil
	}
	switch v.Type().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return e.encodeInt(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return e.encodeUint(v.Uint()), nil
	case reflect.Float32:
		return e.encodeFloat(v.Float(), 32), nil
	case reflect.Float64:
		return e.encodeFloat(v.Float(), 64), nil
	case reflect.Ptr:
		if value := e.encodePtrAnchor(v, column); value != nil {
			return value, nil
		}
		return e.encodeValue(ctx, v.Elem(), column)
	case reflect.Interface:
		return e.encodeValue(ctx, v.Elem(), column)
	case reflect.String:
		return e.encodeString(v.String(), column), nil
	case reflect.Bool:
		return e.encodeBool(v.Bool()), nil
	case reflect.Slice:
		if mapSlice, ok := v.Interface().(yaml.MapSlice); ok {
			return e.encodeMapSlice(ctx, mapSlice, column)
		}
		if value := e.encodePtrAnchor(v, column); value != nil {
			return value, nil
		}
		return e.encodeSlice(ctx, v)
	case reflect.Array:
		return e.encodeArray(ctx, v)
	case reflect.Struct:
		if v.CanInterface() {
			if mapItem, ok := v.Interface().(yaml.MapItem); ok {
				return e.encodeMapItem(ctx, mapItem, column)
			}
			if t, ok := v.Interface().(time.Time); ok {
				return e.encodeTime(t, column), nil
			}
		}
		return e.encodeStruct(ctx, v, column)
	case reflect.Map:
		if value := e.encodePtrAnchor(v, column); value != nil {
			return value, nil
		}
		return e.encodeMap(ctx, v, column)
	default:
		return nil, fmt.Errorf("unknown value type %s", v.Type().String())
	}
}

func (e *ConfigEncoder) encodePtrAnchor(v reflect.Value, column int) ast.Node {
	anchorName, exists := e.getAnchor(v.Pointer())
	if !exists {
		return nil
	}
	aliasName := anchorName
	alias := ast.Alias(token.New("*", "*", e.pos(column)))
	alias.Value = ast.String(token.New(aliasName, aliasName, e.pos(column)))
	e.setSmartAlias(aliasName, v.Pointer())
	return alias
}

func (e *ConfigEncoder) pos(column int) *token.Position {
	return &token.Position{
		Line:        e.line,
		Column:      column,
		Offset:      e.offset,
		IndentNum:   e.indentNum,
		IndentLevel: e.indentLevel,
	}
}

func (e *ConfigEncoder) encodeNil() *ast.NullNode {
	value := "null"
	return ast.Null(token.New(value, value, e.pos(e.column)))
}

func (e *ConfigEncoder) encodeInt(v int64) *ast.IntegerNode {
	value := strconv.FormatInt(v, 10)
	return ast.Integer(token.New(value, value, e.pos(e.column)))
}

func (e *ConfigEncoder) encodeUint(v uint64) *ast.IntegerNode {
	value := strconv.FormatUint(v, 10)
	return ast.Integer(token.New(value, value, e.pos(e.column)))
}

func (e *ConfigEncoder) encodeFloat(v float64, bitSize int) ast.Node {
	if v == math.Inf(0) {
		value := ".inf"
		return ast.Infinity(token.New(value, value, e.pos(e.column)))
	} else if v == math.Inf(-1) {
		value := "-.inf"
		return ast.Infinity(token.New(value, value, e.pos(e.column)))
	} else if math.IsNaN(v) {
		value := ".nan"
		return ast.Nan(token.New(value, value, e.pos(e.column)))
	}
	value := strconv.FormatFloat(v, 'f', -1, bitSize)
	if !strings.Contains(value, ".") && !strings.Contains(value, "e") {
		if e.autoInt {
			return ast.Integer(token.New(value, value, e.pos(e.column)))
		}
		// append x.0 suffix to keep float value context
		value = fmt.Sprintf("%s.0", value)
	}
	return ast.Float(token.New(value, value, e.pos(e.column)))
}

func (e *ConfigEncoder) isNeedQuoted(v string) bool {
	if e.isJSONStyle {
		return true
	}
	if e.useLiteralStyleIfMultiline && strings.ContainsAny(v, "\n\r") {
		return false
	}
	if e.isFlowStyle && strings.ContainsAny(v, `]},'"`) {
		return true
	}
	if e.isFlowStyle {
		for i := range len(v) {
			if v[i] != ':' {
				continue
			}
			if i+1 < len(v) && v[i+1] == '/' {
				continue
			}
			return true
		}
	}
	if token.IsNeedQuoted(v) {
		return true
	}
	return false
}

func (e *ConfigEncoder) encodeString(v string, column int) *ast.StringNode {
	if e.isNeedQuoted(v) {
		if e.singleQuote {
			v = quoteWith(v, '\'')
		} else {
			v = strconv.Quote(v)
		}
	}
	return ast.String(token.New(v, v, e.pos(column)))
}

func (e *ConfigEncoder) encodeBool(v bool) *ast.BoolNode {
	value := strconv.FormatBool(v)
	return ast.Bool(token.New(value, value, e.pos(e.column)))
}

func (e *ConfigEncoder) encodeSlice(ctx context.Context, value reflect.Value) (*ast.SequenceNode, error) {
	if e.indentSequence {
		e.column += e.indentNum
		defer func() { e.column -= e.indentNum }()
	}
	column := e.column
	sequence := ast.Sequence(token.New("-", "-", e.pos(column)), e.isFlowStyle)
	for i := range value.Len() {
		node, err := e.encodeValue(ctx, value.Index(i), column)
		if err != nil {
			return nil, err
		}
		sequence.Values = append(sequence.Values, node)
	}
	return sequence, nil
}

func (e *ConfigEncoder) encodeArray(ctx context.Context, value reflect.Value) (*ast.SequenceNode, error) {
	if e.indentSequence {
		e.column += e.indentNum
		defer func() { e.column -= e.indentNum }()
	}
	column := e.column
	sequence := ast.Sequence(token.New("-", "-", e.pos(column)), e.isFlowStyle)
	for i := range value.Len() {
		node, err := e.encodeValue(ctx, value.Index(i), column)
		if err != nil {
			return nil, err
		}
		sequence.Values = append(sequence.Values, node)
	}
	return sequence, nil
}

func (e *ConfigEncoder) encodeMapItem(ctx context.Context, item yaml.MapItem, column int) (*ast.MappingValueNode, error) {
	k := reflect.ValueOf(item.Key)
	v := reflect.ValueOf(item.Value)
	value, err := e.encodeValue(ctx, v, column)
	if err != nil {
		return nil, err
	}
	if e.isMapNode(value) {
		value.AddColumn(e.indentNum)
	}
	if e.isTagAndMapNode(value) {
		value.AddColumn(e.indentNum)
	}
	return ast.MappingValue(
		token.New("", "", e.pos(column)),
		e.encodeString(k.Interface().(string), column),
		value,
	), nil
}

func (e *ConfigEncoder) encodeMapSlice(ctx context.Context, value yaml.MapSlice, column int) (*ast.MappingNode, error) {
	node := ast.Mapping(token.New("", "", e.pos(column)), e.isFlowStyle)
	for _, item := range value {
		encoded, err := e.encodeMapItem(ctx, item, column)
		if err != nil {
			return nil, err
		}
		node.Values = append(node.Values, encoded)
	}
	return node, nil
}

func (e *ConfigEncoder) isMapNode(node ast.Node) bool {
	_, ok := node.(ast.MapNode)
	return ok
}

func (e *ConfigEncoder) isTagAndMapNode(node ast.Node) bool {
	tn, ok := node.(*ast.TagNode)
	return ok && e.isMapNode(tn.Value)
}

func (e *ConfigEncoder) encodeMap(ctx context.Context, value reflect.Value, column int) (ast.Node, error) {
	node := ast.Mapping(token.New("", "", e.pos(column)), e.isFlowStyle)
	keys := make([]any, len(value.MapKeys()))
	for i, k := range value.MapKeys() {
		keys[i] = k.Interface()
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j])
	})
	for _, key := range keys {
		k := reflect.ValueOf(key)
		v := value.MapIndex(k)
		encoded, err := e.encodeValue(ctx, v, column)
		if err != nil {
			return nil, err
		}
		if e.isMapNode(encoded) {
			encoded.AddColumn(e.indentNum)
		}
		if e.isTagAndMapNode(encoded) {
			encoded.AddColumn(e.indentNum)
		}
		keyText := fmt.Sprint(key)
		vRef := e.toPointer(v)

		// during the second encoding, an anchor is assigned if it is found to be used by an alias.
		if aliasName, exists := e.getSmartAlias(vRef); exists {
			anchorName := aliasName
			anchorNode := ast.Anchor(token.New("&", "&", e.pos(column)))
			anchorNode.Name = ast.String(token.New(anchorName, anchorName, e.pos(column)))
			anchorNode.Value = encoded
			encoded = anchorNode
		}
		node.Values = append(node.Values, ast.MappingValue(
			nil,
			e.encodeString(keyText, column),
			encoded,
		))
		e.setSmartAnchor(vRef, keyText)
	}
	return node, nil
}

// IsZeroer is used to check whether an object is zero to determine
// whether it should be omitted when marshaling with the omitempty flag.
// One notable implementation is time.Time.
type IsZeroer interface {
	IsZero() bool
}

func (e *ConfigEncoder) isOmittedByOmitZero(v reflect.Value) bool {
	kind := v.Kind()
	if z, ok := v.Interface().(IsZeroer); ok {
		if (kind == reflect.Ptr || kind == reflect.Interface) && v.IsNil() {
			return true
		}
		return z.IsZero()
	}
	switch kind {
	case reflect.String:
		return len(v.String()) == 0
	case reflect.Interface, reflect.Ptr, reflect.Slice, reflect.Map:
		return v.IsNil()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Struct:
		vt := v.Type()
		for i := v.NumField() - 1; i >= 0; i-- {
			if vt.Field(i).PkgPath != "" {
				continue // private field
			}
			if !e.isOmittedByOmitZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

func (e *ConfigEncoder) isOmittedByOmitEmptyOption(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return len(v.String()) == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Bool:
		return !v.Bool()
	}
	return false
}

// The current implementation of the omitempty tag combines the functionality of encoding/json's omitempty and omitzero tags.
// This stems from a historical decision to respect the implementation of gopkg.in/yaml.v2, but it has caused confusion,
// so we are working to integrate it into the functionality of encoding/json. (However, this will take some time.)
// In the current implementation, in addition to the exclusion conditions of omitempty,
// if a type implements IsZero, that implementation will be used.
// Furthermore, for non-pointer structs, if all fields are eligible for exclusion,
// the struct itself will also be excluded. These behaviors are originally the functionality of omitzero.
func (e *ConfigEncoder) isOmittedByOmitEmptyTag(v reflect.Value) bool {
	kind := v.Kind()
	if z, ok := v.Interface().(IsZeroer); ok {
		if (kind == reflect.Ptr || kind == reflect.Interface) && v.IsNil() {
			return true
		}
		return z.IsZero()
	}
	switch kind {
	case reflect.String:
		return len(v.String()) == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Struct:
		vt := v.Type()
		for i := v.NumField() - 1; i >= 0; i-- {
			if vt.Field(i).PkgPath != "" {
				continue // private field
			}
			if !e.isOmittedByOmitEmptyTag(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

func (e *ConfigEncoder) encodeTime(v time.Time, column int) *ast.StringNode {
	value := v.Format(time.RFC3339Nano)
	if e.isJSONStyle {
		value = strconv.Quote(value)
	}
	return ast.String(token.New(value, value, e.pos(column)))
}

func (e *ConfigEncoder) encodeDuration(v time.Duration, column int) *ast.StringNode {
	value := v.String()
	if e.isJSONStyle {
		value = strconv.Quote(value)
	}
	return ast.String(token.New(value, value, e.pos(column)))
}

func (e *ConfigEncoder) encodeAnchor(anchorName string, value ast.Node, fieldValue reflect.Value, column int) (*ast.AnchorNode, error) {
	anchorNode := ast.Anchor(token.New("&", "&", e.pos(column)))
	anchorNode.Name = ast.String(token.New(anchorName, anchorName, e.pos(column)))
	anchorNode.Value = value
	if e.anchorCallback != nil {
		if err := e.anchorCallback(anchorNode, fieldValue.Interface()); err != nil {
			return nil, err
		}
		if snode, ok := anchorNode.Name.(*ast.StringNode); ok {
			anchorName = snode.Value
		}
	}
	if fieldValue.Kind() == reflect.Ptr {
		e.setAnchor(fieldValue.Pointer(), anchorName)
	}
	return anchorNode, nil
}

func (e *ConfigEncoder) encodeStruct(ctx context.Context, value reflect.Value, column int) (ast.Node, error) {
	node := ast.Mapping(token.New("", "", e.pos(column)), e.isFlowStyle)
	structType := value.Type()
	fieldMap, err := structFieldMap(structType)
	if err != nil {
		return nil, err
	}
	hasInlineAnchorField := false
	var inlineAnchorValue reflect.Value
	for i := range value.NumField() {
		field := structType.Field(i)
		if isIgnoredStructField(field) {
			continue
		}
		fieldValue := value.FieldByName(field.Name)
		sf := fieldMap[field.Name]
		if (e.omitZero || sf.IsOmitZero) && e.isOmittedByOmitZero(fieldValue) {
			// omit encoding by omitzero tag or OmitZero option.
			continue
		}
		if e.omitEmpty && e.isOmittedByOmitEmptyOption(fieldValue) {
			// omit encoding by OmitEmpty option.
			continue
		}
		if sf.IsOmitEmpty && e.isOmittedByOmitEmptyTag(fieldValue) {
			// omit encoding by omitempty tag.
			continue
		}
		ve := e
		if !e.isFlowStyle && sf.IsFlow {
			ve = &ConfigEncoder{}
			*ve = *e
			ve.isFlowStyle = true
		}
		encoded, err := ve.encodeValue(ctx, fieldValue, column)
		if err != nil {
			return nil, err
		}
		if e.isMapNode(encoded) {
			encoded.AddColumn(e.indentNum)
		}
		var key ast.MapKeyNode = e.encodeString(sf.RenderName, column)
		switch {
		case encoded.Type() == ast.AliasType:
			if aliasName := sf.AliasName; aliasName != "" {
				alias, ok := encoded.(*ast.AliasNode)
				if !ok {
					return nil, fmt.Errorf("failed to encode")
				}
				got := alias.Value.String()
				if aliasName != got {
					return nil, fmt.Errorf("expected alias name is %q but got %q", aliasName, got)
				}
			}
			if sf.IsInline {
				// if both used alias and inline, output `<<: *alias`
				key = ast.MergeKey(token.New("<<", "<<", e.pos(column)))
			}
		case sf.AnchorName != "":
			anchorNode, err := e.encodeAnchor(sf.AnchorName, encoded, fieldValue, column)
			if err != nil {
				return nil, err
			}
			encoded = anchorNode
		case sf.IsInline:
			isAutoAnchor := sf.IsAutoAnchor
			if !hasInlineAnchorField {
				hasInlineAnchorField = isAutoAnchor
			}
			if isAutoAnchor {
				inlineAnchorValue = fieldValue
			}
			mapNode, ok := encoded.(ast.MapNode)
			if !ok {
				// if an inline field is null, skip encoding it
				if _, ok := encoded.(*ast.NullNode); ok {
					continue
				}
				return nil, errors.New("inline value is must be map or struct type")
			}
			mapIter := mapNode.MapRange()
			for mapIter.Next() {
				mapKey := mapIter.Key()
				mapValue := mapIter.Value()
				keyName := mapKey.GetToken().Value
				if fieldMap.isIncludedRenderName(keyName) {
					// if declared the same key name, skip encoding this field
					continue
				}
				mapKey.AddColumn(-e.indentNum)
				mapValue.AddColumn(-e.indentNum)
				node.Values = append(node.Values, ast.MappingValue(nil, mapKey, mapValue))
			}
			continue
		case sf.IsAutoAnchor:
			anchorNode, err := e.encodeAnchor(sf.RenderName, encoded, fieldValue, column)
			if err != nil {
				return nil, err
			}
			encoded = anchorNode
		}
		node.Values = append(node.Values, ast.MappingValue(nil, key, encoded))
	}
	if hasInlineAnchorField {
		node.AddColumn(e.indentNum)
		anchorName := "anchor"
		anchorNode := ast.Anchor(token.New("&", "&", e.pos(column)))
		anchorNode.Name = ast.String(token.New(anchorName, anchorName, e.pos(column)))
		anchorNode.Value = node
		if e.anchorCallback != nil {
			if err := e.anchorCallback(anchorNode, value.Addr().Interface()); err != nil {
				return nil, err
			}
			if snode, ok := anchorNode.Name.(*ast.StringNode); ok {
				anchorName = snode.Value
			}
		}
		if inlineAnchorValue.Kind() == reflect.Ptr {
			e.setAnchor(inlineAnchorValue.Pointer(), anchorName)
		}
		return anchorNode, nil
	}
	return node, nil
}

func (e *ConfigEncoder) toPointer(v reflect.Value) uintptr {
	if e.isInvalidValue(v) {
		return 0
	}

	switch v.Type().Kind() {
	case reflect.Ptr:
		return v.Pointer()
	case reflect.Interface:
		return e.toPointer(v.Elem())
	case reflect.Slice:
		return v.Pointer()
	case reflect.Map:
		return v.Pointer()
	}
	return 0
}

func (e *ConfigEncoder) clearSmartAnchorRef() {
	if !e.enableSmartAnchor {
		return
	}
	e.anchorRefToName = make(map[uintptr]string)
	e.anchorNameMap = make(map[string]struct{})
}

func (e *ConfigEncoder) setSmartAnchor(ptr uintptr, name string) {
	if !e.enableSmartAnchor {
		return
	}
	e.setAnchor(ptr, e.generateAnchorName(name))
}

func (e *ConfigEncoder) setAnchor(ptr uintptr, name string) {
	if ptr == 0 {
		return
	}
	if name == "" {
		return
	}
	e.anchorRefToName[ptr] = name
	e.anchorNameMap[name] = struct{}{}
}

func (e *ConfigEncoder) generateAnchorName(base string) string {
	if _, exists := e.anchorNameMap[base]; !exists {
		return base
	}
	for i := 1; i < 100; i++ {
		name := base + strconv.Itoa(i)
		if _, exists := e.anchorNameMap[name]; exists {
			continue
		}
		return name
	}
	return ""
}

func (e *ConfigEncoder) getAnchor(ref uintptr) (string, bool) {
	anchorName, exists := e.anchorRefToName[ref]
	return anchorName, exists
}

func (e *ConfigEncoder) setSmartAlias(name string, ref uintptr) {
	if !e.enableSmartAnchor {
		return
	}
	e.aliasRefToName[ref] = name
}

func (e *ConfigEncoder) getSmartAlias(ref uintptr) (string, bool) {
	if !e.enableSmartAnchor {
		return "", false
	}
	aliasName, exists := e.aliasRefToName[ref]
	return aliasName, exists
}

const (
	// StructTagName tag keyword for Marshal/Unmarshal
	StructTagName = "yaml"
)

// StructField information for each the field in structure
type StructField struct {
	FieldName    string
	RenderName   string
	AnchorName   string
	AliasName    string
	IsAutoAnchor bool
	IsAutoAlias  bool
	IsOmitEmpty  bool
	IsOmitZero   bool
	IsFlow       bool
	IsInline     bool
}

func getTag(field reflect.StructField) string {
	// If struct tag `yaml` exist, use that. If no `yaml`
	// exists, but `json` does, use that and try the best to
	// adhere to its rules
	tag := field.Tag.Get(StructTagName)
	if tag == "" {
		tag = field.Tag.Get(`json`)
	}
	return tag
}

func structField(field reflect.StructField) *StructField {
	tag := getTag(field)
	fieldName := strings.ToLower(field.Name)
	options := strings.Split(tag, ",")
	if len(options) > 0 {
		if options[0] != "" {
			fieldName = options[0]
		}
	}
	sf := &StructField{
		FieldName:  field.Name,
		RenderName: fieldName,
	}
	if len(options) > 1 {
		for _, opt := range options[1:] {
			switch {
			case opt == "omitempty":
				sf.IsOmitEmpty = true
			case opt == "omitzero":
				sf.IsOmitZero = true
			case opt == "flow":
				sf.IsFlow = true
			case opt == "inline":
				sf.IsInline = true
			case strings.HasPrefix(opt, "anchor"):
				anchor := strings.Split(opt, "=")
				if len(anchor) > 1 {
					sf.AnchorName = anchor[1]
				} else {
					sf.IsAutoAnchor = true
				}
			case strings.HasPrefix(opt, "alias"):
				alias := strings.Split(opt, "=")
				if len(alias) > 1 {
					sf.AliasName = alias[1]
				} else {
					sf.IsAutoAlias = true
				}
			default:
			}
		}
	}
	return sf
}

func isIgnoredStructField(field reflect.StructField) bool {
	if field.PkgPath != "" && !field.Anonymous {
		// private field
		return true
	}
	return getTag(field) == "-"
}

// StructFieldMap is a goccy/go-yaml type
type StructFieldMap map[string]*StructField

func (m StructFieldMap) isIncludedRenderName(name string) bool {
	for _, v := range m {
		if !v.IsInline && v.RenderName == name {
			return true
		}
	}
	return false
}

func structFieldMap(structType reflect.Type) (StructFieldMap, error) {
	fieldMap := StructFieldMap{}
	renderNameMap := map[string]struct{}{}
	for i := range structType.NumField() {
		field := structType.Field(i)
		if isIgnoredStructField(field) {
			continue
		}
		sf := structField(field)
		if _, exists := renderNameMap[sf.RenderName]; exists {
			return nil, fmt.Errorf("duplicated struct field name %s", sf.RenderName)
		}
		fieldMap[sf.FieldName] = sf
		renderNameMap[sf.RenderName] = struct{}{}
	}
	return fieldMap, nil
}
