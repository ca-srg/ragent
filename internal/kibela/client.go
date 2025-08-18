package kibela

import (
	"context"
	"fmt"

	"github.com/machinebox/graphql"
)

type Client struct {
	client *graphql.Client
	token  string
	team   string
}

type Note struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Content     string           `json:"content"`
	PublishedAt string           `json:"publishedAt"`
	URL         string           `json:"url"`
	Author      Author           `json:"author"`
	Folders     FolderConnection `json:"folders"`
}

type Author struct {
	Account string `json:"account"`
}

type Group struct {
	Name string `json:"name"`
}

type Folder struct {
	FullName string `json:"fullName"`
}

type FolderConnection struct {
	Nodes []Folder `json:"nodes"`
}

type PageInfo struct {
	HasNextPage     bool   `json:"hasNextPage"`
	HasPreviousPage bool   `json:"hasPreviousPage"`
	StartCursor     string `json:"startCursor"`
	EndCursor       string `json:"endCursor"`
}

type NotesConnection struct {
	Edges []struct {
		Node Note `json:"node"`
	} `json:"edges"`
	PageInfo   PageInfo `json:"pageInfo"`
	TotalCount int      `json:"totalCount"`
}

type NotesResponse struct {
	Notes NotesConnection `json:"notes"`
}

func NewClient(team, token string) *Client {
	apiURL := fmt.Sprintf("https://%s.kibe.la/api/v1", team)
	client := graphql.NewClient(apiURL)

	return &Client{
		client: client,
		token:  token,
		team:   team,
	}
}

// GetNoteURL generates the full Kibela page URL for a given note ID
func (c *Client) GetNoteURL(note Note) string {
	return note.URL
}

func (c *Client) GetAllNotes(ctx context.Context) ([]Note, error) {
	var allNotes []Note
	var cursor *string

	for {
		notes, pageInfo, err := c.fetchNotes(ctx, cursor)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch notes: %w", err)
		}

		allNotes = append(allNotes, notes...)

		if !pageInfo.HasNextPage {
			break
		}
		cursor = &pageInfo.EndCursor
	}

	return allNotes, nil
}

func (c *Client) fetchNotes(ctx context.Context, cursor *string) ([]Note, PageInfo, error) {
	query := `
		query GetNotes($first: Int!, $after: String) {
			notes(first: $first, after: $after, orderBy: {field: PUBLISHED_AT, direction: DESC}) {
				edges {
					node {
						id
						title
						content
						publishedAt
						url
						author {
							account
						}
						folders(first: 10) {
							nodes {
								fullName
							}
						}
					}
				}
				pageInfo {
					hasNextPage
					hasPreviousPage
					startCursor
					endCursor
				}
				totalCount
			}
		}
	`

	req := graphql.NewRequest(query)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Var("first", 100)

	if cursor != nil {
		req.Var("after", *cursor)
	}

	var resp NotesResponse
	err := c.client.Run(ctx, req, &resp)
	if err != nil {
		return nil, PageInfo{}, fmt.Errorf("GraphQL query failed: %w", err)
	}

	notes := make([]Note, len(resp.Notes.Edges))
	for i, edge := range resp.Notes.Edges {
		notes[i] = edge.Node
	}

	return notes, resp.Notes.PageInfo, nil
}
