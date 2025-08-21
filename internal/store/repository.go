package store

import (
	"database/sql"
	"mycoder/internal/models"
)

// TxRunner provides a transaction wrapper for repository operations.
type TxRunner interface {
	WithTx(fn func(*sql.Tx) error) error
}

// ProjectRepo defines minimal project CRUD.
type ProjectRepo interface {
	CreateProject(name, root string, ignore []string) *models.Project
	ListProjects() []*models.Project
	GetProject(id string) (*models.Project, bool)
	UpdateProjectName(id, name string) error
	DeleteProject(id string) error
}

// DocumentRepo defines minimal document CRUD.
type DocumentRepo interface {
	AddDocument(projectID, path, content string) *models.Document
	UpsertDocument(projectID, path, content, sha, lang string) *models.Document
	GetDocument(projectID, path string) (*models.Document, bool)
	DeleteDocument(projectID, path string) error
}
