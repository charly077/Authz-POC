package templates

import (
	"html/template"
	"net/http"
	"strings"
	"time"
)

type PageData struct {
	Username   string
	Roles      string
	RoleList   []string
	Metadata   string
	Path       string
	Method     string
	Time       string
	IsPublic   bool
	Decision   string
	StatusIcon string
}

type DossiersPageData struct {
	Username string
}

var (
	Page     *template.Template
	Dossiers *template.Template
)

func Init(templateDir string) {
	Page = template.Must(template.New("home.html").ParseFiles(templateDir + "/home.html"))
	Dossiers = template.Must(template.New("dossiers.html").ParseFiles(templateDir + "/dossiers.html"))
}

func BuildPageData(r *http.Request, isPublic bool) PageData {
	user := r.Header.Get("x-current-user")
	roles := r.Header.Get("x-user-role")
	metadata := r.Header.Get("x-user-metadata")

	var roleList []string
	if roles != "" {
		for _, role := range strings.Split(roles, ",") {
			role = strings.TrimSpace(role)
			if role != "" {
				roleList = append(roleList, role)
			}
		}
	}

	decision := metadata
	if decision == "" {
		decision = "N/A"
	}

	statusIcon := "\u2705"
	if isPublic {
		statusIcon = "\U0001F310"
	}

	return PageData{
		Username:   user,
		Roles:      roles,
		RoleList:   roleList,
		Metadata:   metadata,
		Path:       r.URL.Path,
		Method:     r.Method,
		Time:       time.Now().Format(time.RFC3339),
		IsPublic:   isPublic,
		Decision:   decision,
		StatusIcon: statusIcon,
	}
}
