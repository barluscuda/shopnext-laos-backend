package application

import (
	"embed"
	"encoding/json"
)

//go:embed geography.json
var geographyFS embed.FS

type Province struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Districts []string `json:"districts"`
}

func Geography() []Province {
	raw, err := geographyFS.ReadFile("geography.json")
	if err != nil {
		panic(err)
	}
	var result []Province
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}
func ValidDistrict(province, district string) bool {
	for _, p := range Geography() {
		if p.Name == province {
			for _, d := range p.Districts {
				if d == district {
					return true
				}
			}
		}
	}
	return false
}
