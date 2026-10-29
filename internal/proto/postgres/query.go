package postgres

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var tableRE = regexp.MustCompile(`(?i)^(?:create table|insert into|drop table)\s+"?([a-zA-Z0-9_]+)"?`)
var fromRE = regexp.MustCompile(`(?i)\s+from\s+"?([a-zA-Z0-9_]+)"?\s*$`)
var aliasRE = regexp.MustCompile(`(?i)\s+as\s+"?([a-zA-Z0-9_?]+)"?$`)

func (s *server) query(sql string, params []*string, db string, execute bool) result {
	normalized := strings.TrimSuffix(normalizeSQL(sql), ";")
	lower := strings.ToLower(normalized)
	for _, r := range s.cfg.Queries {
		if r.re.MatchString(normalized) {
			return result{columns: r.Columns, rows: r.Rows, code: r.Error}
		}
	}
	if strings.Contains(lower, "pg_catalog.pg_attribute") {
		return s.metadata(params, db)
	}
	if strings.HasPrefix(lower, "select") && strings.Contains(lower, "pg_catalog.pg_type") {
		typeName, typeID := "jsonb", "3802"
		if len(params) > 0 && params[0] != nil {
			switch *params[0] {
			case "2950", "uuid":
				typeName, typeID = "uuid", "2950"
			case "23", "int4":
				typeName, typeID = "int4", "23"
			case "1043", "varchar":
				typeName, typeID = "varchar", "1043"
			}
		}
		if strings.Contains(lower, "current_schemas(true)") {
			return result{columns: []column{{"?column?", 16}, {"nspname", 25}, {"typname", 25}}, rows: [][]*string{{str("t"), str("pg_catalog"), str(typeName)}}}
		}
		return result{columns: []column{{"is_array", 16}, {"typtype", 25}, {"typname", 25}, {"oid", 23}}, rows: [][]*string{{str("f"), str("b"), str(typeName), str(typeID)}}}
	}
	if strings.HasPrefix(lower, "show ") {
		name := strings.TrimSpace(normalized[5:])
		value := "on"
		switch strings.ToLower(name) {
		case "transaction isolation level", "transaction_isolation":
			value = "read committed"
		case "server_version":
			value = "16.0"
		case "search_path":
			value = "public"
		}
		return scalar(name, 25, str(value))
	}
	if strings.HasPrefix(lower, "explain ") {
		return scalar("QUERY PLAN", 25, str("Seq Scan (mock; no execution plan)"))
	}
	if match := tableRE.FindStringSubmatch(normalized); match != nil {
		name := strings.ToLower(match[1])
		tag := "CREATE TABLE"
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case strings.HasPrefix(lower, "create"):
			if execute {
				s.tables[name] = [][]*string{}
			}
		case strings.HasPrefix(lower, "drop"):
			tag = "DROP TABLE"
			if execute {
				delete(s.tables, name)
			}
		case strings.HasPrefix(lower, "insert"):
			tag = "INSERT 0 1"
			idx := strings.Index(lower, "values")
			if idx >= 0 {
				vals := strings.TrimSpace(normalized[idx+6:])
				vals = strings.TrimSuffix(strings.TrimPrefix(vals, "("), ")")
				parts := splitSQL(vals, ',')
				if len(parts) == 2 && execute {
					a, _, _ := expression(parts[0], params, db)
					b, _, _ := expression(parts[1], params, db)
					s.tables[name] = append(s.tables[name], []*string{a, b})
				}
			}
		}
		return result{tag: tag}
	}
	if match := fromRE.FindStringSubmatch(normalized); match != nil && strings.HasPrefix(lower, "select ") {
		name := strings.ToLower(match[1])
		s.mu.Lock()
		defer s.mu.Unlock()
		rows, exists := s.tables[name]
		if exists {
			selection := strings.TrimSpace(normalized[7:strings.Index(lower, " from ")])
			fields := splitSQL(selection, ',')
			if selection == "*" {
				fields = []string{"id", "label"}
			}
			r := result{}
			for _, f := range fields {
				f = strings.Trim(strings.TrimSpace(f), "\"")
				if f != "id" && f != "label" {
					return result{code: "42703"}
				}
				t := uint32(25)
				if f == "id" {
					t = 23
				}
				r.columns = append(r.columns, column{f, t})
			}
			for _, row := range rows {
				values := []*string{}
				for _, c := range r.columns {
					idx := 0
					if c.Name == "label" {
						idx = 1
					}
					values = append(values, row[idx])
				}
				r.rows = append(r.rows, values)
			}
			return r
		}
	}
	if strings.HasPrefix(lower, "select ") && !strings.Contains(lower, " from ") {
		r := result{}
		row := []*string{}
		for _, part := range splitSQL(normalized[7:], ',') {
			name := "?column?"
			if m := aliasRE.FindStringSubmatch(part); m != nil {
				name = m[1]
				part = part[:len(part)-len(m[0])]
			}
			value, t, ok := expression(part, params, db)
			if !ok {
				return s.unknown()
			}
			if name == "?column?" && strings.Contains(part, "(") {
				name = strings.TrimSpace(strings.Split(part, "(")[0])
			}
			r.columns = append(r.columns, column{name, t})
			row = append(row, value)
		}
		r.rows = [][]*string{row}
		return r
	}
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return result{tag: "EMPTY"}
	}
	switch fields[0] {
	case "select", "with":
		return s.unknown()
	case "begin", "start":
		return result{tag: "BEGIN"}
	case "commit", "end":
		return result{tag: "COMMIT"}
	case "rollback":
		return result{tag: "ROLLBACK"}
	case "insert":
		return result{tag: "INSERT 0 1"}
	case "update", "delete":
		return result{tag: strings.ToUpper(fields[0]) + " 1"}
	default:
		return result{tag: strings.ToUpper(fields[0])}
	}
}
func (s *server) unknown() result {
	if s.cfg.UnknownSelectError {
		return result{code: "0A000"}
	}
	return result{columns: []column{{"?column?", 25}}}
}
func scalar(name string, t uint32, value *string) result {
	return result{columns: []column{{name, t}}, rows: [][]*string{{value}}}
}
func expression(raw string, params []*string, db string) (*string, uint32, bool) {
	e := strings.TrimSpace(raw)
	t := uint32(0)
	if idx := strings.LastIndex(e, "::"); idx >= 0 {
		t = oid(strings.TrimSpace(e[idx+2:]))
		e = strings.TrimSpace(e[:idx])
	}
	var value *string
	switch strings.ToLower(e) {
	case "version()":
		value = str("PostgreSQL 16.0 (lmmock)")
	case "current_database()", "current_catalog":
		value = str(db)
	case "current_schema", "current_schema()":
		value = str("public")
	case "null":
		value = nil
	case "true", "false":
		value = str(strings.ToLower(e))
		if t == 0 {
			t = 16
		}
	default:
		if strings.HasPrefix(e, "$") {
			n, err := strconv.Atoi(e[1:])
			if err != nil || n < 1 {
				return nil, 0, false
			}
			if n <= len(params) {
				value = params[n-1]
			}
		} else if len(e) >= 2 && e[0] == '\'' && e[len(e)-1] == '\'' {
			value = str(strings.ReplaceAll(e[1:len(e)-1], "''", "'"))
		} else if _, err := strconv.ParseInt(e, 10, 64); err == nil {
			value = str(e)
			if t == 0 {
				t = 23
			}
		} else {
			return nil, 0, false
		}
	}
	if t == 0 {
		t = 25
	}
	return value, t, true
}
func (s *server) metadata(params []*string, db string) result {
	names := []string{"current_database", "nspname", "relname", "attname", "atttypid", "attnotnull", "atttypmod", "attlen", "typtypmod", "attnum", "attidentity", "attgenerated", "adsrc", "description", "typbasetype", "typtype"}
	r := result{}
	for _, n := range names {
		r.columns = append(r.columns, column{n, 25})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tables := []string{}
	for name := range s.tables {
		tables = append(tables, name)
	}
	sort.Strings(tables)
	for _, name := range tables {
		if len(params) > 0 && params[0] != nil && *params[0] != "%" && *params[0] != name {
			continue
		}
		for i, col := range []string{"id", "label"} {
			typ, mod, length, notnull := "23", "-1", "4", "t"
			if i == 1 {
				typ, mod, length, notnull = "1043", "44", "-1", "f"
			}
			r.rows = append(r.rows, []*string{str(db), str("public"), str(name), str(col), str(typ), str(notnull), str(mod), str(length), str("-1"), str(fmt.Sprint(i + 1)), nil, nil, nil, nil, str("0"), str("b")})
		}
	}
	return r
}

// Normalize query whitespace without changing quoted fixture values.
func normalizeSQL(sql string) string {
	var out strings.Builder
	quote := byte(0)
	space := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if quote != 0 {
			out.WriteByte(c)
			if c == quote {
				if i+1 < len(sql) && sql[i+1] == quote {
					i++
					out.WriteByte(sql[i])
				} else {
					quote = 0
				}
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false
		out.WriteByte(c)
		if c == '\'' || c == '"' {
			quote = c
		}
	}
	return out.String()
}
