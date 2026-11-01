package project

import (
	"errors"
	"time"
)

// ============================================================
// PROJECT
//
// Sub-entity di bawah Tenant (Organization). Satu Tenant bisa
// punya banyak Project — masing-masing Project punya API key dan
// hash chain event sendiri, terisolasi dari Project lain meski
// masih di bawah Tenant yang sama.
// ============================================================

type ProjectStatus string

const (
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusSuspended ProjectStatus = "suspended"
	ProjectStatusDeleted   ProjectStatus = "deleted"
)

type Project struct {
	ID                string        `json:"id"`
	TenantID          string        `json:"tenant_id"`
	Name              string        `json:"name"`
	Status            ProjectStatus `json:"status"`
	MonthlyEventLimit int64         `json:"monthly_event_limit"`
	RetentionDays     int           `json:"retention_days"`
	CreatedAt         time.Time     `json:"created_at"`
	DeletedAt         *time.Time    `json:"deleted_at,omitempty"`
}

// DefaultMonthlyEventLimit — dipakai sampai Fase 3 (billing) punya
// tier plan sungguhan yang menentukan limit per customer.
const (
	DefaultMonthlyEventLimit = 10000
	// DefaultRetentionDays — fallback untuk tenant Free/belum-checkout SAJA.
	// Pro/Business dapat retention dari PlanResolver (180/365 hari), tidak
	// lewat konstanta ini. Nilai ini HARUS sama dengan janji pricing publik
	// §1 (Free tier: 14 hari) — diperbaiki dari 90 ke 14 (2026-09-14).
	DefaultRetentionDays = 14
)

var (
	ErrNotFound     = errors.New("project_not_found")
	ErrNameRequired = errors.New("project_name_required")
)

func NewProject(tenantID, name string) (*Project, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id_required")
	}
	if name == "" {
		return nil, ErrNameRequired
	}
	return &Project{
		TenantID:          tenantID,
		Name:              name,
		Status:            ProjectStatusActive,
		MonthlyEventLimit: DefaultMonthlyEventLimit,
		RetentionDays:     DefaultRetentionDays,
		CreatedAt:         time.Now(),
	}, nil
}
