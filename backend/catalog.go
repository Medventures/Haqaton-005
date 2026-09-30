package main

import (
	"encoding/json"
	"os"
)

type Specialty struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Treats      []string `json:"treats"`
}

type Service struct {
	ID          string `json:"id"`
	SpecialtyID string `json:"specialty_id"`
	Name        string `json:"name"`
	Price       int    `json:"price"`
	Description string `json:"description"`
}

type Doctor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	SpecialtyID string   `json:"specialty_id"`
	Slots       []string `json:"slots"`
}

type Catalog struct {
	Specialties []Specialty `json:"specialties"`
	Services    []Service   `json:"services"`
	Doctors     []Doctor    `json:"doctors"`
}

func LoadCatalog(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	return &c, json.Unmarshal(b, &c)
}

func (c *Catalog) Specialty(id string) *Specialty {
	for i := range c.Specialties {
		if c.Specialties[i].ID == id {
			return &c.Specialties[i]
		}
	}
	return nil
}

func (c *Catalog) ServicesFor(specID string) []Service {
	out := []Service{}
	for _, s := range c.Services {
		if s.SpecialtyID == specID {
			out = append(out, s)
		}
	}
	return out
}

// DoctorsFor returns only doctors that have free slots.
func (c *Catalog) DoctorsFor(specID string) []Doctor {
	out := []Doctor{}
	for _, d := range c.Doctors {
		if d.SpecialtyID == specID && len(d.Slots) > 0 {
			out = append(out, d)
		}
	}
	return out
}
