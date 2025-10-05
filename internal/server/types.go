package server

type Versions struct {
	Texts          string `json:"texts"`
	Textcatalog    string `json:"textcatalog,omitempty"`
	Citedata       string `json:"citedata,omitempty"`
	Citecatalog    string `json:"citecatalog,omitempty"`
	Citerelations  string `json:"citerelations,omitempty"`
	Citeextensions string `json:"citeextensions,omitempty"`
	DSE            string `json:"dse,omitempty"`
	ORCA           string `json:"orca,omitempty"`
}

type CITEResponse struct {
	Status   string   `json:"status"`
	Service  string   `json:"service"`
	Versions Versions `json:"versions"`
}

type VersionResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type Node struct {
	URN      []string `json:"urn"`
	Text     []string `json:"text,omitempty"`
	Previous []string `json:"previous,omitempty"`
	Next     []string `json:"next,omitempty"`
	Sequence int      `json:"sequence"`
	Complete bool     `json:"complete"`
}

type NodeResponse struct {
	RequestUrn []string `json:"requestUrn"`
	Status     string   `json:"status"`
	Service    string   `json:"service"`
	Message    string   `json:"message,omitempty"`
	URN        []string `json:"urns,omitempty"`
	Nodes      []Node   `json:"nodes,omitempty"`
}

type URNResponse struct {
	RequestUrn []string `json:"requestUrn"`
	Status     string   `json:"status"`
	Service    string   `json:"service"`
	Message    string   `json:"message,omitempty"`
	URN        []string `json:"urns,omitempty"`
}

type CatalogEntry struct {
	URN            string `json:"urn"`
	CitationScheme string `json:"citationScheme"`
	GroupName      string `json:"groupName"`
	WorkTitle      string `json:"workTitle"`
	VersionLabel   string `json:"versionLabel,omitempty"`
	ExemplarLabel  string `json:"exemplarLabel,omitempty"`
	Online         bool   `json:"online"`
}

type CatalogResponse struct {
	Status  string         `json:"status"`
	Service string         `json:"service"`
	Entries []CatalogEntry `json:"entries"`
	Message string         `json:"message,omitempty"`
}

type ServerConfig struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	Source     string `json:"cex_source"`      // file OR directory base
	TestSource string `json:"test_cex_source"` // fallback for /texts without CEX
}
