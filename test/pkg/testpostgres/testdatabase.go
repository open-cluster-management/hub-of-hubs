package testpostgres

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/stolostron/multicluster-global-hub/pkg/database"
)

func InitDatabase(uri string) error {
	err := database.InitGormInstance(&database.DatabaseConfig{
		URL:      uri,
		Dialect:  database.PostgresDialect,
		PoolSize: 5,
	})
	if err != nil {
		return err
	}

	db := database.GetGorm()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("failed to get current dir: no caller information")
	}
	dirname := filepath.Dir(currentFile)
	dirname = filepath.Dir(dirname)
	dirname = filepath.Dir(dirname)
	dirname = filepath.Dir(dirname)

	sqlDir := filepath.Join(dirname, "operator", "pkg", "controllers", "hubofhubs", "database")
	files, err := os.ReadDir(sqlDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		filePath := filepath.Join(sqlDir, file.Name())
		if file.Name() == "5.privileges.sql" {
			continue
		}
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		result := db.Exec(string(fileContent))
		if result.Error != nil {
			return result.Error
		}
		fmt.Printf("script %s executed successfully.\n", file.Name())
	}

	sqlDir = filepath.Join(dirname, "operator", "pkg", "controllers", "hubofhubs", "upgrade")
	upgradeFiles, err := os.ReadDir(sqlDir)
	if err != nil {
		return err
	}
	for _, file := range upgradeFiles {
		filePath := filepath.Join(sqlDir, file.Name())
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		result := db.Exec(string(fileContent))
		if result.Error != nil {
			return result.Error
		}
		fmt.Printf("script %s executed successfully.\n", file.Name())
	}

	sqlDir = filepath.Join(dirname, "operator", "pkg", "controllers", "hubofhubs", "database.old")
	oldfiles, err := os.ReadDir(sqlDir)
	if err != nil {
		return err
	}
	for _, file := range oldfiles {
		filePath := filepath.Join(sqlDir, file.Name())
		if file.Name() == "5.privileges.sql" {
			continue
		}
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		result := db.Exec(string(fileContent))
		if result.Error != nil {
			return result.Error
		}
		fmt.Printf("script %s executed successfully.\n", file.Name())
	}
	return nil
}
