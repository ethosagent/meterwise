package trim

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// tableMinRows is the fewest rows worth a table: below it the header costs more than it saves.
const tableMinRows = 5

// jsonTable re-encodes a tool result that is a JSON array of flat objects with the same keys in the same order as a
// header and one comma-separated row per object ("[N]{k1,k2}:"), TOON style. Values keep their JSON literals; a string
// that could be read as another type, or contains a comma, quote, backslash or line break, is JSON-quoted. The output
// is parsed back and compared with the input before it is used, so a table is only ever a different way to write the
// same rows.
type jsonTable struct{}

func (jsonTable) Name() contract.TrimStage { return contract.StageJSONTable }

func (jsonTable) Apply(_ Unit, text string) (string, bool) {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '[' || t[len(t)-1] != ']' {
		return "", false
	}
	keys, rows, ok := flatRows(t)
	if !ok || len(rows) < tableMinRows {
		return "", false
	}
	out := encodeTable(keys, rows)
	back, err := DecodeTable(out)
	if err != nil || !sameRows(keys, rows, back) {
		return "", false
	}
	return out, true
}

// cell is one value of a row: its JSON literal, and whether it is a string (then lit is the decoded text).
type cell struct {
	lit string
	str bool
}

// flatRows reads t as an array of flat objects sharing one key order.
func flatRows(t string) (keys []string, rows [][]cell, ok bool) {
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, nil, false
	}
	for dec.More() {
		rowKeys, row, ok := flatObject(dec)
		if !ok || (keys != nil && !equalKeys(keys, rowKeys)) {
			return nil, nil, false
		}
		if keys == nil {
			keys = rowKeys
		}
		rows = append(rows, row)
	}
	if _, err := dec.Token(); err != nil { // the closing bracket
		return nil, nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, false
	}
	return keys, rows, len(keys) > 0 && !dupKeys(keys)
}

// flatObject reads one object whose values are all scalars.
func flatObject(dec *json.Decoder) (keys []string, row []cell, ok bool) {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, false
	}
	for dec.More() {
		kt, err := dec.Token()
		k, isStr := kt.(string)
		if err != nil || !isStr {
			return nil, nil, false
		}
		vt, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		c, ok := scalar(vt)
		if !ok {
			return nil, nil, false
		}
		keys, row = append(keys, k), append(row, c)
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, nil, false
	}
	return keys, row, true
}

func scalar(tok json.Token) (cell, bool) {
	switch v := tok.(type) {
	case string:
		return cell{v, true}, true
	case json.Number:
		return cell{v.String(), false}, true
	case bool:
		return cell{strconv.FormatBool(v), false}, true
	case nil:
		return cell{"null", false}, true
	}
	return cell{}, false // a nested object or array
}

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dupKeys(keys []string) bool {
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

// literal reports whether s reads as a JSON number, true, false or null.
func literal(s string) bool {
	if s == "true" || s == "false" || s == "null" {
		return true
	}
	n := json.Number(s)
	_, err := n.Float64()
	return err == nil
}

// quoteIf returns s as written in a table: bare when that reads back as the same string, else a JSON string.
func quoteIf(s string) string {
	if s == "" || s != strings.TrimSpace(s) || strings.ContainsAny(s, ",\"\\\n\r\t:{}[]") || literal(s) {
		return quoteJSON(s)
	}
	return s
}

func encodeTable(keys []string, rows [][]cell) string {
	var b strings.Builder
	b.WriteString("[" + itoa(len(rows)) + "]{")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteIf(k))
	}
	b.WriteString("}:")
	for _, r := range rows {
		b.WriteByte('\n')
		for i, c := range r {
			if i > 0 {
				b.WriteByte(',')
			}
			if c.str {
				b.WriteString(quoteIf(c.lit))
			} else {
				b.WriteString(c.lit)
			}
		}
	}
	return b.String()
}

// Table is a decoded jsonTable output: the column names and the rows.
type Table struct {
	keys []string
	rows [][]cell
}

// DecodeTable parses the text jsonTable writes. It is the inverse the stage verifies against, exported so tests can
// show a table re-parses to the original rows.
func DecodeTable(s string) (Table, error) {
	lines := strings.Split(s, "\n")
	head := lines[0]
	end := strings.Index(head, "]{")
	if len(head) < 5 || head[0] != '[' || end < 0 || !strings.HasSuffix(head, "}:") {
		return Table{}, errors.New("trim: not a table")
	}
	n, err := strconv.Atoi(head[1:end])
	if err != nil || n != len(lines)-1 {
		return Table{}, errors.New("trim: row count does not match")
	}
	keyCells, err := splitCells(head[end+2 : len(head)-2])
	if err != nil {
		return Table{}, err
	}
	t := Table{}
	for _, k := range keyCells {
		t.keys = append(t.keys, k.lit)
	}
	for _, l := range lines[1:] {
		row, err := splitCells(l)
		if err != nil || len(row) != len(t.keys) {
			return Table{}, errors.New("trim: bad row")
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

// splitCells splits one table line at its commas, reading a quoted cell as a JSON string and a bare cell as a JSON
// literal when it is one and as a string otherwise.
func splitCells(l string) ([]cell, error) {
	var out []cell
	for i := 0; ; {
		c, next, err := nextCell(l, i)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		i = next
		if i >= len(l) {
			return out, nil
		}
		if l[i] != ',' {
			return nil, errors.New("trim: expected a comma")
		}
		i++
		if i == len(l) { // a trailing comma ends with an empty bare cell
			return append(out, cell{"", true}), nil
		}
	}
}

// nextCell reads the cell starting at l[i] and returns it with the index just past it.
func nextCell(l string, i int) (cell, int, error) {
	if i < len(l) && l[i] == '"' {
		end := i + 1
		for end < len(l) && l[end] != '"' {
			if l[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(l) {
			return cell{}, 0, errors.New("trim: unterminated quote")
		}
		var s string
		if err := json.Unmarshal([]byte(l[i:end+1]), &s); err != nil {
			return cell{}, 0, err
		}
		return cell{s, true}, end + 1, nil
	}
	end := strings.IndexByte(l[i:], ',')
	if end < 0 {
		end = len(l) - i
	}
	bare := l[i : i+end]
	return cell{bare, !literal(bare)}, i + end, nil
}

func sameRows(keys []string, want [][]cell, got Table) bool {
	if !equalKeys(keys, got.keys) || len(want) != len(got.rows) {
		return false
	}
	for i := range want {
		for j := range want[i] {
			if want[i][j] != got.rows[i][j] {
				return false
			}
		}
	}
	return true
}

// JSON writes the table back as the compact JSON array of objects it encodes.
func (t Table) JSON() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, r := range t.rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for j, c := range r {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quoteJSON(t.keys[j]) + ":")
			if c.str {
				b.WriteString(quoteJSON(c.lit))
			} else {
				b.WriteString(c.lit)
			}
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

func quoteJSON(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}
