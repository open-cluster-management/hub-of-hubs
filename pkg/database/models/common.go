package models

import "time"

type ResourceVersion struct {
	Key             string `gorm:"column:key"`
	ResourceVersion string `gorm:"column:resource_version"`
}

type Table struct {
	Schema string `gorm:"column:schema_name"`
	Table  string `gorm:"column:table_name"`
}

// This is for time column from database
type Time struct {
	Time time.Time `gorm:"column:time"`
}
