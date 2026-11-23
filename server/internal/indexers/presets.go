package indexers

// Preset is a well-known Newznab (usenet) indexer: pick it, paste the API
// key from the indexer's profile page, done.
type Preset struct {
	ID      string
	Name    string
	BaseURL string
	APIPath string
	SiteURL string
}

// UsenetPresets are usenet indexers whose API addresses were taken from
// their public documentation and checked to answer t=caps (2026-11-23).
var UsenetPresets = []Preset{
	{ID: "nzbgeek", Name: "NZBgeek", BaseURL: "https://api.nzbgeek.info", APIPath: "/api", SiteURL: "https://nzbgeek.info"},
	{ID: "drunkenslug", Name: "DrunkenSlug", BaseURL: "https://drunkenslug.com", APIPath: "/api", SiteURL: "https://drunkenslug.com"},
	{ID: "nzbplanet", Name: "NZBPlanet", BaseURL: "https://api.nzbplanet.net", APIPath: "/api", SiteURL: "https://nzbplanet.net"},
	{ID: "nzbfinder", Name: "NZBFinder", BaseURL: "https://nzbfinder.ws", APIPath: "/api", SiteURL: "https://nzbfinder.ws"},
	{ID: "dognzb", Name: "DOGnzb", BaseURL: "https://api.dognzb.cr", APIPath: "/api", SiteURL: "https://dognzb.cr"},
	{ID: "althub", Name: "altHUB", BaseURL: "https://api.althub.co.za", APIPath: "/api", SiteURL: "https://althub.co.za"},
	{ID: "tabularasa", Name: "Tabula Rasa", BaseURL: "https://www.tabula-rasa.pw", APIPath: "/api/v1/api", SiteURL: "https://www.tabula-rasa.pw"},
	{ID: "ninjacentral", Name: "NinjaCentral", BaseURL: "https://ninjacentral.co.za", APIPath: "/api", SiteURL: "https://ninjacentral.co.za"},
	{ID: "miatrix", Name: "Miatrix", BaseURL: "https://www.miatrix.com", APIPath: "/api", SiteURL: "https://www.miatrix.com"},
	{ID: "usenetcrawler", Name: "Usenet Crawler", BaseURL: "https://www.usenet-crawler.com", APIPath: "/api", SiteURL: "https://www.usenet-crawler.com"},
}
