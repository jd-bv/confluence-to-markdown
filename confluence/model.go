package confluence

type Page struct {
	Id       string `json:"id"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Children *struct {
		Page *struct {
			Results []struct {
				Links struct {
					Self string `json:"self"`
				} `json:"_links"`
			} `json:"results"`
		}
	} `json:"children,omitempty"`

	Body struct {
		ExportView struct {
			Value string `json:"value"`
		} `json:"export_view"`
	} `json:"body"`
}

type Space struct {
	Id         int    `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Expandable struct {
		Settings    string `json:"settings"`
		Metadata    string `json:"metadata"`
		Operations  string `json:"operations"`
		LookAndFeel string `json:"lookAndFeel"`
		Identifiers string `json:"identifiers"`
		Permissions string `json:"permissions"`
		Icon        string `json:"icon"`
		Description string `json:"description"`
		Theme       string `json:"theme"`
		History     string `json:"history"`
		Homepage    string `json:"homepage"`
	} `json:"_expandable"`
	Links struct {
		Webui string `json:"webui"`
		Self  string `json:"self"`
	} `json:"_links"`
}
