package plex

import (
	"context"
	"net/url"
	"regexp"
)

var machinePattern = regexp.MustCompile(`^[A-Za-z0-9]{8,64}$`)
var ratingKeyPattern = regexp.MustCompile(`^[0-9]{1,12}$`)

// machineID reads the server's identifier. A failure is not a sync failure: the
// items just have no link to open them in Plex Web.
func (c *Client) machineID(ctx context.Context) string {
	res, err := c.get(ctx, "/identity", nil)
	if err != nil || !machinePattern.MatchString(res.MediaContainer.MachineIdentifier) {
		return ""
	}
	return res.MediaContainer.MachineIdentifier
}

// webLink is where Plex Web shows one item. It carries no credential.
func (c *Client) webLink(machine, ratingKey string) string {
	if machine == "" || !ratingKeyPattern.MatchString(ratingKey) {
		return ""
	}
	return c.baseURL + "/web/index.html#!/server/" + machine + "/details?key=" + url.QueryEscape("/library/metadata/"+ratingKey)
}
