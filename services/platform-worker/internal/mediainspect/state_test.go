package mediainspect

import (
	"fmt"
	"testing"
)

func preparedSnapshot() Snapshot {
	s := Snapshot{Links: []Link{{ID: "link-a", AssetID: "asset-a", EntityType: "work"}}}
	for _, rendition := range []string{"master", "w320", "w640", "w960"} {
		o := Object{ID: rendition, AssetID: "asset-a", AssetType: "work_image", AssetStatus: "published", RightsStatus: "allowed",
			Version: 1, CurrentVersion: 1, Scope: "public", Key: "media-public/a/" + rendition + ".webp", Rendition: rendition, Status: "published"}
		address := "https://media.example.test/" + o.Key
		o.PublicURL = &address
		if rendition == "master" {
			o.Scope, o.Key, o.Status, o.PublicURL = "private", "media-master/a/master.webp", "ready", nil
		}
		s.Objects = append(s.Objects, o)
	}
	return s
}

func TestPublicationStatePreparationSharingAndRetirement(t *testing.T) {
	for _, scenario := range []string{"prepared-draft", "prepared-hidden-parent", "shared", "retained-private-master", "public-deletion-pending", "deleted", "no-image-default"} {
		t.Run(scenario, func(t *testing.T) {
			s := preparedSnapshot()
			switch scenario {
			case "shared":
				s.Links = append(s.Links, Link{ID: "link-b", AssetID: "asset-a", EntityType: "work"})
			case "retained-private-master":
				s.Objects[0].Status = "hidden"
			case "public-deletion-pending", "deleted":
				s.Links = nil
				for i := range s.Objects {
					s.Objects[i].AssetStatus = "hidden"
					s.Objects[i].Status, s.Objects[i].PublicURL, s.Objects[i].DeletionQueued = "hidden", nil, true
					if scenario == "deleted" {
						s.Objects[i].Status = "deleted"
					}
				}
			case "no-image-default":
				s = Snapshot{}
			}
			if report := Inspect(s); report.PublicationIssues != 0 {
				t.Fatalf("normal lifecycle reported as an issue: %+v", report)
			}
		})
	}
}

func TestPublicationStateViolations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Object)
	}{
		{"scope", func(o *Object) { o.Scope = "private" }},
		{"wrong-prefix", func(o *Object) { o.Key = "media-master/a/w320.webp" }},
		{"key-traversal", func(o *Object) { o.Key = "media-public/../w320.webp" }},
		{"encoded-key", func(o *Object) { o.Key = "media-public/%2e%2e/w320.webp" }},
		{"unreviewed-public", func(o *Object) { o.AssetStatus, o.RightsStatus = "reviewing", "needs_review" }},
		{"rights-takedown", func(o *Object) { o.AssetStatus, o.RightsStatus = "takedown", "takedown" }},
		{"old-version", func(o *Object) { o.CurrentVersion++ }},
		{"missing-url", func(o *Object) { o.PublicURL = nil }},
		{"untrusted-url", func(o *Object) { v := "https://user:secret@media.test/" + o.Key; o.PublicURL = &v }},
		{"wrong-url-key", func(o *Object) { v := "https://media.test/media-public/other.webp"; o.PublicURL = &v }},
		{"url-query", func(o *Object) { v := *o.PublicURL + "?"; o.PublicURL = &v }},
		{"url-fragment", func(o *Object) { v := *o.PublicURL + "#"; o.PublicURL = &v }},
		{"hidden-with-url", func(o *Object) { o.Status, o.DeletionQueued = "hidden", true }},
		{"hidden-without-live-delete", func(o *Object) { o.Status, o.PublicURL = "hidden", nil }},
		{"deleted-with-url", func(o *Object) { o.Status = "deleted" }},
		{"unapproved-public-upload", func(o *Object) { o.Status, o.PublicURL = "pending", nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := preparedSnapshot()
			test.change(&s.Objects[1])
			report := Inspect(s)
			if report.PublicationIssues != 2 || len(report.IssueSamples["object_state"]) != 1 || len(report.IssueSamples["link_state"]) != 1 {
				t.Fatalf("expected one object and one affected link: %+v", report)
			}
		})
	}
	s := preparedSnapshot()
	s.Links[0].EntityType = "performer"
	if Inspect(s).PublicationIssues != 1 {
		t.Fatal("wrong asset type linked")
	}
	s = preparedSnapshot()
	s.Objects[0].PublicURL = s.Objects[1].PublicURL
	if Inspect(s).PublicationIssues != 1 {
		t.Fatal("private exposure not reported")
	}
}

func TestSamplesAreBoundedAndCountsStayComplete(t *testing.T) {
	s := Snapshot{}
	for i := range 125 {
		s.Objects = append(s.Objects, Object{ID: fmt.Sprint(i)})
	}
	report := Inspect(s)
	if report.PublicationIssues != 125 || len(report.IssueSamples["object_state"]) != 50 || report.ObjectCount != 125 {
		t.Fatalf("counts were truncated with samples: %+v", report)
	}
}
