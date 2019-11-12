package person_api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
)

type Client struct {
	clientId     string
	clientSecret string
	accessToken  string
	httpClient   *http.Client
	baseUrl      string
	authUrl      string

	rwLock *sync.RWMutex
}

func NewClient(id, secret, baseUrl, authUrl string) (*Client, error) {
	httpClient := &http.Client{}
	c := &Client{
		httpClient:   httpClient,
		clientId:     id,
		clientSecret: secret,
		baseUrl:      baseUrl,
		authUrl:      authUrl,
		rwLock:       &sync.RWMutex{},
	}
	err := c.RefreshAccessToken()
	if err != nil {
		return nil, err
	}
	return c, nil
}

type getMethod int

const (
	USERID           getMethod = 0
	UUID             getMethod = 1
	PRIMARY_EMAIL    getMethod = 2
	PRIMARY_USERNAME getMethod = 3
)

func (c *Client) RefreshAccessToken() error {
	c.rwLock.Lock()
	defer c.rwLock.Unlock()
	accessToken, err := c.GetAccessToken(c.authUrl)
	if err != nil {
		return err
	}
	c.accessToken = accessToken
	return nil
}

func (c *Client) GetAccessToken(authUrl string) (string, error) {
	// TODO: Support passing in audience, scope, etc.
	authReqBody, err := json.Marshal(AuthReq{
		Audience:     "api.sso.mozilla.com",
		Scope:        "classification:public display:public",
		GrantType:    "client_credentials",
		ClientId:     c.clientId,
		ClientSecret: c.clientSecret})
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Post(authUrl, "application/json", bytes.NewBuffer(authReqBody))
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Persons API responded with status code %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var authResp AuthResp
	err = json.Unmarshal(body, &authResp)
	if err != nil {
		return "", err
	}

	return authResp.AccessToken, nil
}

type usersResp struct {
	Items    []*Person `json:"Items"`
	NextPage string    `json:"nextPage"`
}

func (c *Client) GetAllUsers() ([]*Person, error) {
	var (
		allUsers []*Person
		nextPage string
		req      *http.Request
		err      error
	)

	c.rwLock.RLock()
	defer c.rwLock.RUnlock()
	for {
		if nextPage == "" {
			req, err = http.NewRequest("GET", c.baseUrl+"/v2/users", nil)
		} else {
			req, err = http.NewRequest("GET", fmt.Sprintf("%s/v2/users?nextPage={\"id\":\"%s\"}", c.baseUrl, nextPage), nil)
		}
		if err != nil {
			return nil, err
		}
		req.Header.Add("Authorization", "Bearer "+c.accessToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("Persons API responded with status code %d", resp.StatusCode)
		}

		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		var uResp usersResp
		err = json.Unmarshal(body, &uResp)
		if err != nil {
			return nil, err
		}

		for _, i := range uResp.Items {
			allUsers = append(allUsers, i)
		}

		if uResp.NextPage == "None" {
			break
		}
		nextPage = uResp.NextPage
	}

	return allUsers, nil
}

func (c *Client) getPerson(method getMethod, id string) (*Person, error) {
	url := c.baseUrl + "/v2/user"

	if method == USERID {
		url = url + "/user_id/" + id
	} else if method == UUID {
		url = url + "/uuid/" + id
	} else if method == PRIMARY_EMAIL {
		url = url + "/primary_email/" + id
	} else if method == PRIMARY_USERNAME {
		url = url + "/primary_username/" + id
	} else {
		return nil, fmt.Errorf("Unknown method type")
	}

	c.rwLock.RLock()
	defer c.rwLock.RUnlock()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Persons API responded with status code %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	p, err := UnmarshalPerson(body)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

func (c *Client) GetPersonByUserId(userid string) (*Person, error) {
	return c.getPerson(USERID, userid)
}
func (c *Client) GetPersonByUUID(uuid string) (*Person, error) {
	return c.getPerson(UUID, uuid)
}
func (c *Client) GetPersonByEmail(primaryEmail string) (*Person, error) {
	return c.getPerson(PRIMARY_EMAIL, primaryEmail)
}

func (c *Client) GetPersonByUsername(primaryUsername string) (*Person, error) {
	return c.getPerson(PRIMARY_USERNAME, primaryUsername)
}

func (c *Client) GetPersonsInGroups(groups []string) ([]*Person, error) {
	collectedPersons := []*Person{}
	persons, err := c.GetAllUsers()
	if err != nil {
		return collectedPersons, err
	}
	for _, person := range persons {
		done := false
		for group := range person.AccessInformation.LDAP.Values {
			for _, specifiedGroup := range groups {
				if group == specifiedGroup {
					collectedPersons = append(collectedPersons, person)
					done = true
				}

				if done {
					break
				}
			}

			if done {
				break
			}
		}
	}
	return collectedPersons, nil
}

type AuthReq struct {
	Audience     string `json:"audience"`
	Scope        string `json:"scope"`
	GrantType    string `json:"grant_type"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type AuthResp struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}
