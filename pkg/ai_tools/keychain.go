package ai_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type KeychainClient struct {
	BaseURL      string
	RuntimeToken string
	HTTPClient   *http.Client
}

type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Model struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Name       string `json:"name"`
}

type RuntimeUser struct {
	ID             string `json:"id"`
	ChannelName    string `json:"channelName"`
	ExternalUserID string `json:"externalUserId"`
	Name           string `json:"name"`
	IsEnabled      bool   `json:"isEnabled"`
}

type UpsertExternalUserRequest struct {
	ChannelName    string
	ExternalUserID string
	Name           string
	IsEnabled      bool
}

type HostedUserCredentials struct {
	ChannelName string
	Username    string
	Name        string
	Password    string
}

type DispatchRequest struct {
	ChannelName string `json:"channelName"`
	UserID      string `json:"userId"`
	ProviderID  string `json:"providerId"`
	ModelID     string `json:"modelId"`
}

type DispatchResult struct {
	DispatchLogID string `json:"dispatchLogId"`
	ProviderName  string `json:"providerName"`
	ModelName     string `json:"modelName"`
	KeyID         string `json:"keyId"`
	KeyAlias      string `json:"keyAlias"`
	Key           string `json:"key"`
}

func (c KeychainClient) DispatchByName(ctx context.Context, channelName, userID, providerName, modelName string) (DispatchResult, error) {
	provider, err := c.FindProvider(ctx, providerName)
	if err != nil {
		return DispatchResult{}, err
	}
	model, err := c.FindModel(ctx, provider.ID, modelName)
	if err != nil {
		return DispatchResult{}, err
	}
	return c.Dispatch(ctx, DispatchRequest{
		ChannelName: channelName,
		UserID:      userID,
		ProviderID:  provider.ID,
		ModelID:     model.ID,
	})
}

func (c KeychainClient) UpsertExternalUser(ctx context.Context, input UpsertExternalUserRequest) (RuntimeUser, error) {
	if input.ChannelName == "" {
		return RuntimeUser{}, fmt.Errorf("keychain channel name is required")
	}
	if input.ExternalUserID == "" {
		return RuntimeUser{}, fmt.Errorf("keychain external user id is required")
	}
	if input.Name == "" {
		input.Name = input.ExternalUserID
	}

	body := map[string]any{
		"name":      input.Name,
		"isEnabled": input.IsEnabled,
	}
	var out RuntimeUser
	path := "/api/runtime/channels/" + url.PathEscape(input.ChannelName) + "/external-users/" + url.PathEscape(input.ExternalUserID)
	if err := c.do(ctx, http.MethodPut, path, body, &out); err != nil {
		return RuntimeUser{}, err
	}
	return out, nil
}

func (c KeychainClient) RegisterHostedUser(ctx context.Context, input HostedUserCredentials) (RuntimeUser, error) {
	if input.ChannelName == "" {
		return RuntimeUser{}, fmt.Errorf("keychain channel name is required")
	}
	if input.Username == "" {
		return RuntimeUser{}, fmt.Errorf("keychain hosted username is required")
	}
	if input.Password == "" {
		return RuntimeUser{}, fmt.Errorf("keychain hosted password is required")
	}
	if input.Name == "" {
		input.Name = input.Username
	}

	body := map[string]string{
		"username": input.Username,
		"name":     input.Name,
		"password": input.Password,
	}
	var out RuntimeUser
	path := "/api/runtime/channels/" + url.PathEscape(input.ChannelName) + "/hosted-users/register"
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return RuntimeUser{}, err
	}
	return out, nil
}

func (c KeychainClient) LoginHostedUser(ctx context.Context, input HostedUserCredentials) (RuntimeUser, error) {
	if input.ChannelName == "" {
		return RuntimeUser{}, fmt.Errorf("keychain channel name is required")
	}
	if input.Username == "" {
		return RuntimeUser{}, fmt.Errorf("keychain hosted username is required")
	}
	if input.Password == "" {
		return RuntimeUser{}, fmt.Errorf("keychain hosted password is required")
	}

	body := map[string]string{
		"username": input.Username,
		"password": input.Password,
	}
	var out RuntimeUser
	path := "/api/runtime/channels/" + url.PathEscape(input.ChannelName) + "/hosted-users/login"
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return RuntimeUser{}, err
	}
	return out, nil
}

func (c KeychainClient) FindProvider(ctx context.Context, name string) (Provider, error) {
	var providers []Provider
	if err := c.do(ctx, http.MethodGet, "/api/runtime/providers", nil, &providers); err != nil {
		return Provider{}, err
	}
	for _, provider := range providers {
		if strings.EqualFold(provider.Name, name) {
			return provider, nil
		}
	}
	return Provider{}, fmt.Errorf("keychain provider %q not found", name)
}

func (c KeychainClient) FindModel(ctx context.Context, providerID, name string) (Model, error) {
	var models []Model
	path := "/api/runtime/models?providerId=" + url.QueryEscape(providerID)
	if err := c.do(ctx, http.MethodGet, path, nil, &models); err != nil {
		return Model{}, err
	}
	for _, model := range models {
		if strings.EqualFold(model.Name, name) {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("keychain model %q not found for provider %s", name, providerID)
}

func (c KeychainClient) Dispatch(ctx context.Context, input DispatchRequest) (DispatchResult, error) {
	var out DispatchResult
	if err := c.do(ctx, http.MethodPost, "/api/runtime/dispatches", input, &out); err != nil {
		return DispatchResult{}, err
	}
	return out, nil
}

func (c KeychainClient) ReportFailure(ctx context.Context, dispatchLogID, errorCode, errorMessage string) error {
	body := map[string]string{
		"errorCode":    errorCode,
		"errorMessage": errorMessage,
	}
	path := "/api/runtime/dispatches/" + url.PathEscape(dispatchLogID) + "/failure"
	return c.do(ctx, http.MethodPost, path, body, nil)
}

func (c KeychainClient) do(ctx context.Context, method, path string, in any, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("keychain base url is required")
	}
	if c.RuntimeToken == "" {
		return fmt.Errorf("keychain runtime token is required")
	}

	var body *bytes.Reader
	if in == nil {
		body = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.RuntimeToken)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var responseBody bytes.Buffer
	if _, err := responseBody.ReadFrom(resp.Body); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Provider: "keychain", Status: resp.StatusCode, Body: responseBody.String()}
	}
	if out == nil || responseBody.Len() == 0 {
		return nil
	}
	return json.Unmarshal(responseBody.Bytes(), out)
}
