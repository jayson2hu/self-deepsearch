package operations

import (
	"context"
	"time"
)

type TrafficDay struct {
	Date      string `json:"date"`
	PageViews int64  `json:"page_views"`
}

type TrafficContent struct {
	Rank       int    `json:"rank"`
	EntityType string `json:"entity_type"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	PageViews  int64  `json:"page_views"`
}

type TrafficUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	PageViews int64  `json:"page_views"`
}

type TrafficReport struct {
	Range                 string           `json:"range"`
	CheckedAt             time.Time        `json:"checked_at"`
	Timezone              string           `json:"timezone"`
	PageViews             int64            `json:"page_views"`
	TodayPageViews        int64            `json:"today_page_views"`
	LoggedInUserViews     int64            `json:"logged_in_user_views"`
	ActiveRegisteredUsers int64            `json:"active_registered_users"`
	UniqueVisitors        *int64           `json:"unique_visitors"`
	Daily                 []TrafficDay     `json:"daily"`
	HotContent            []TrafficContent `json:"hot_content"`
	Users                 []TrafficUser    `json:"users"`
}

type TrafficRepository interface {
	TrafficReport(context.Context, string, time.Time) (TrafficReport, error)
}
