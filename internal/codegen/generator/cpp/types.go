package cpp

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/Alia5/VIIPER/internal/codegen/meta"
	"github.com/Alia5/VIIPER/internal/codegen/scanner"
)

const typesTemplate = `{{.Header}}
#pragma once

#include "config.hpp"
#include "detail/json.hpp"
#include <string>
#include <vector>
#include <optional>
#include <cstdint>

namespace viiper {

{{range .DTOs}}
struct {{pascalcase .Name}};
{{end}}

// ============================================================================
// Management API DTOs
// ============================================================================

{{range .DTOs}}
// {{.Name}}
struct {{pascalcase .Name}} {
{{- range .Fields}}
        {{fieldcpptype .}} {{camelcase .Name}};
{{- end}}

    static {{pascalcase .Name}} from_json(const json_type& j) {
        {{pascalcase .Name}} result{};
{{- range .Fields}}
{{- if and .Optional (eq .TypeKind "map")}}
        if (j.contains("{{.JSONName}}") && !j["{{.JSONName}}"].is_null()) {
            result.{{camelcase .Name}} = j["{{.JSONName}}"];
        } else {
            result.{{camelcase .Name}} = std::nullopt;
        }
{{- else if and .Optional (eq .TypeKind "slice")}}
        if (j.contains("{{.JSONName}}") && !j["{{.JSONName}}"].is_null()) {
            result.{{camelcase .Name}} = detail::get_array<{{cpptype .Type | sliceElementType}}>(j, "{{.JSONName}}");
        } else {
            result.{{camelcase .Name}} = std::nullopt;
        }
{{- else if and .Optional (isCustomType .Type)}}
        if (j.contains("{{.JSONName}}") && !j["{{.JSONName}}"].is_null()) {
            result.{{camelcase .Name}} = {{fieldcpptype . | unwrapOptional}}::from_json(j["{{.JSONName}}"]);
        } else {
            result.{{camelcase .Name}} = std::nullopt;
        }
{{- else if .Optional}}
        result.{{camelcase .Name}} = detail::get_optional_field<{{fieldcpptype . | unwrapOptional}}>(j, "{{.JSONName}}");
{{- else if eq .TypeKind "slice"}}
        result.{{camelcase .Name}} = detail::get_array<{{cpptype .Type | sliceElementType}}>(j, "{{.JSONName}}");
{{- else if eq .TypeKind "map"}}
        if (j.contains("{{.JSONName}}")) {
            result.{{camelcase .Name}} = j["{{.JSONName}}"];
        }
{{- else if isCustomType .Type}}
        if (j.contains("{{.JSONName}}")) {
            result.{{camelcase .Name}} = {{cpptype .Type}}::from_json(j["{{.JSONName}}"]);
        }
{{- else}}
        result.{{camelcase .Name}} = j.value("{{.JSONName}}", {{cpptype .Type}}{});
{{- end}}
{{- end}}
        return result;
    }

    [[nodiscard]] json_type to_json() const {
        json_type j;
{{- range .Fields}}
{{- if and .Optional (eq .TypeKind "slice")}}
        if ({{camelcase .Name}}.has_value()) {
            json_type arr = json_type::array();
            for (const auto& item : {{camelcase .Name}}.value()) {
                {{- if isCustomType .Type}}
                arr.push_back(item.to_json());
                {{- else}}
                arr.push_back(item);
                {{- end}}
            }
            j["{{.JSONName}}"] = std::move(arr);
        }
{{- else if .Optional}}
        if ({{camelcase .Name}}.has_value()) {
            {{- if isCustomType .Type}}
            j["{{.JSONName}}"] = {{camelcase .Name}}.value().to_json();
            {{- else}}
            j["{{.JSONName}}"] = {{camelcase .Name}}.value();
            {{- end}}
        }
{{- else if eq .TypeKind "slice"}}
        {
            json_type arr = json_type::array();
            for (const auto& item : {{camelcase .Name}}) {
                {{- if isCustomType .Type}}
                arr.push_back(item.to_json());
                {{- else}}
                arr.push_back(item);
                {{- end}}
            }
            j["{{.JSONName}}"] = std::move(arr);
        }
{{- else if isCustomType .Type}}
        j["{{.JSONName}}"] = {{camelcase .Name}}.to_json();
{{- else}}
        j["{{.JSONName}}"] = {{camelcase .Name}};
{{- end}}
{{- end}}
        return j;
    }
};

{{end}}

} // namespace viiper
`

func generateTypes(logger *slog.Logger, includeDir string, md *meta.Metadata) error {
	logger.Debug("Generating types.hpp")
	outputFile := filepath.Join(includeDir, "types.hpp")

	funcs := tplFuncs(md)

	tmpl := template.Must(template.New("types").Funcs(funcs).Parse(typesTemplate))

	f, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("create types.hpp: %w", err)
	}
	defer f.Close() //nolint:errcheck

	data := struct {
		Header string
		DTOs   []scanner.DTOSchema
	}{
		Header: writeFileHeader(),
		DTOs:   orderDTOsForCPlusPlus(md.DTOs),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("execute types template: %w", err)
	}

	logger.Info("Generated types.hpp", "file", outputFile)
	return nil
}

// orderDTOsForCPlusPlus emits dependent DTOs before the DTOs that contain
// them. C++ needs complete element types for vectors and inline to_json()
// methods; forward declarations alone are insufficient for nested response
// objects such as ServerStatusResponse -> ServerStatusBus -> ServerStatusDevice.
func orderDTOsForCPlusPlus(dtos []scanner.DTOSchema) []scanner.DTOSchema {
	byName := make(map[string]scanner.DTOSchema, len(dtos))
	order := make(map[string]int, len(dtos))
	for index, dto := range dtos {
		byName[dto.Name] = dto
		order[dto.Name] = index
	}

	dependencies := func(dto scanner.DTOSchema) []string {
		seen := make(map[string]struct{})
		for _, field := range dto.Fields {
			name := strings.TrimPrefix(field.Type, "*")
			name = strings.TrimPrefix(name, "[]")
			if strings.HasPrefix(name, "map[") {
				if value := strings.LastIndex(name, "]"); value >= 0 {
					name = name[value+1:]
				}
			}
			if _, ok := byName[name]; ok && name != dto.Name {
				seen[name] = struct{}{}
			}
		}
		result := make([]string, 0, len(seen))
		for name := range seen {
			result = append(result, name)
		}
		sort.SliceStable(result, func(i, j int) bool { return order[result[i]] < order[result[j]] })
		return result
	}

	state := make(map[string]uint8, len(dtos))
	result := make([]scanner.DTOSchema, 0, len(dtos))
	var visit func(string)
	visit = func(name string) {
		switch state[name] {
		case 2:
			return
		case 1:
			// Recursive DTOs cannot be represented by an inline C++ value
			// without a pointer; retain the stable order rather than looping.
			return
		}
		state[name] = 1
		for _, dependency := range dependencies(byName[name]) {
			visit(dependency)
		}
		state[name] = 2
		result = append(result, byName[name])
	}
	for _, dto := range dtos {
		visit(dto.Name)
	}
	return result
}
