package mediainspect

import (
	"net/url"
	"strings"
)

const maximumInventory = 10000

type Object struct {
	ID, AssetID, AssetType, AssetStatus, RightsStatus string
	Version, CurrentVersion                           int
	Scope, Key, Rendition, Status                     string
	PublicURL                                         *string
	DeletionQueued                                    bool
}

type Link struct{ ID, AssetID, EntityType string }
type Snapshot struct {
	Objects []Object
	Links   []Link
}

// Inspect counts each inconsistent object/link once, with bounded UUID-only
// samples. Parent visibility is deliberately not a precondition: approved
// images may be prepared for a draft/hidden parent and shared across parents.
func Inspect(snapshot Snapshot) Report {
	r := Report{ObjectCount: len(snapshot.Objects), LinkCount: len(snapshot.Links),
		IssueSamples: map[string][]string{"object_state": {}, "link_state": {}}}
	usable := make(map[string]map[string]bool)
	assetTypes := make(map[string]string)
	for _, object := range snapshot.Objects {
		if !validObject(object) {
			r.addIssue("object_state", object.ID)
			continue
		}
		if object.Scope == "public" && object.Status == "published" {
			if usable[object.AssetID] == nil {
				usable[object.AssetID] = make(map[string]bool)
			}
			usable[object.AssetID][object.Rendition] = true
			assetTypes[object.AssetID] = object.AssetType
		}
	}
	for _, link := range snapshot.Links {
		typeOK := (link.EntityType == "work" && assetTypes[link.AssetID] == "work_image") ||
			(link.EntityType == "performer" && assetTypes[link.AssetID] == "performer_avatar")
		renditions := usable[link.AssetID]
		if !typeOK || !renditions["w320"] || !renditions["w640"] || !renditions["w960"] {
			r.addIssue("link_state", link.ID)
		}
	}
	return r
}

func (r *Report) addIssue(kind, id string) {
	r.PublicationIssues++
	if len(r.IssueSamples[kind]) < 50 {
		r.IssueSamples[kind] = append(r.IssueSamples[kind], id)
	}
}

func validObject(o Object) bool {
	if strings.ContainsAny(o.Key, "\\\r\n\x00%?#") {
		return false
	}
	for _, segment := range strings.Split(o.Key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	if o.Scope == "private" {
		// Retained masters, including future deletion tasks, are expected.
		return strings.HasPrefix(o.Key, "media-master/") && (o.Rendition == "master" || o.Rendition == "quarantine") &&
			o.PublicURL == nil && o.Status != "published"
	}
	if o.Scope != "public" || !strings.HasPrefix(o.Key, "media-public/") ||
		(o.Rendition != "w320" && o.Rendition != "w640" && o.Rendition != "w960") {
		return false
	}
	switch o.Status {
	case "deleted":
		// Physical orphans are handled by storage reconciliation, not here.
		return o.PublicURL == nil
	case "hidden":
		return o.PublicURL == nil && o.DeletionQueued
	case "published":
		if o.AssetStatus != "published" || o.RightsStatus != "allowed" || o.Version != o.CurrentVersion || o.PublicURL == nil {
			return false
		}
		u, err := url.Parse(*o.PublicURL)
		return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
			u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.Contains(*o.PublicURL, "#") &&
			strings.HasSuffix(u.Path, "/"+o.Key) && u.RawPath == ""
	default:
		// Unapproved/staging content belongs under the private prefix.
		return false
	}
}
