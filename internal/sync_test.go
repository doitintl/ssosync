// Copyright (c) 2020, Amazon.com, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package internal

import (
	"testing"

	"github.com/awslabs/ssosync/internal/config"
	"github.com/awslabs/ssosync/internal/interfaces"
	"github.com/stretchr/testify/assert"
	admin "google.golang.org/api/admin/directory/v1"
)

func TestMatchIgnorePattern(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		// Exact match (no wildcard).
		{"foo@example.com", "foo@example.com", true},
		{"foo@example.com", "bar@example.com", false},

		// Leading wildcard (the *@domain case this PR is about).
		{"*@example.com", "alice@example.com", true},
		{"*@example.com", "bob@example.com", true},
		{"*@example.com", "alice@other.com", false},
		{"*@example.com", "@example.com", true},  // '*' matches empty
		{"*@example.com", "example.com", false}, // missing '@'

		// Trailing wildcard.
		{"svc-*", "svc-prod", true},
		{"svc-*", "svc-", true},
		{"svc-*", "other", false},

		// Middle wildcard.
		{"a*z", "az", true},
		{"a*z", "abcz", true},
		{"a*z", "abc", false},
		{"a*z", "zab", false},

		// Multiple wildcards.
		{"*-temp-*", "x-temp-y", true},
		{"*-temp-*", "-temp-", true},
		{"*-temp-*", "temp", false},

		// Edge patterns.
		{"*", "anything", true},
		{"*", "", true},
		{"**", "anything", true},

		// Other characters from path.Match are literal here.
		{"foo?bar", "foo?bar", true}, // '?' is literal, not single-char wildcard
		{"foo?bar", "fooXbar", false},
		{"[abc]", "[abc]", true}, // brackets are literal
		{"[abc]", "a", false},
		{`a\*b`, `a\Xb`, true},  // pattern is literal 'a\' + wildcard + 'b'
		{`a\*b`, "aXb", false},  // backslash is required literally
		{`a\*b`, `a\b`, true},   // '*' matches the empty string
	}

	for _, c := range cases {
		got := matchIgnorePattern(c.pattern, c.name)
		assert.Equalf(t, c.want, got, "matchIgnorePattern(%q, %q)", c.pattern, c.name)
	}
}

func TestIgnoreUserWildcard(t *testing.T) {
	s := &syncGSuite{
		cfg: &config.Config{
			IgnoreUsers: []string{
				"  *@internal.example.com  ", // whitespace should be trimmed
				"exact@example.com",
				"", // empty entries should be dropped
			},
		},
	}

	cases := []struct {
		name string
		want bool
	}{
		{"alice@internal.example.com", true},
		{"bob@internal.example.com", true},
		{"exact@example.com", true},
		{"alice@external.example.com", false},
		{"  alice@internal.example.com  ", true}, // input whitespace trimmed
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, s.ignoreUser(c.name), "ignoreUser(%q)", c.name)
	}
}

func TestIgnoreGroupWildcard(t *testing.T) {
	s := &syncGSuite{
		cfg: &config.Config{
			IgnoreGroups: []string{"AWS*", "exact-group"},
		},
	}
	cases := []struct {
		name string
		want bool
	}{
		{"AWSAccountFactory", true},
		{"AWSServiceRole", true},
		{"exact-group", true},
		{"OtherGroup", false},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, s.ignoreGroup(c.name), "ignoreGroup(%q)", c.name)
	}
}

func TestGetGroupOperationsRespectsIgnore(t *testing.T) {
	ignore := func(name string) bool {
		return name == "AWSReserved" || name == "ManualGroup"
	}

	awsGroups := []*interfaces.Group{
		{DisplayName: "GroupInBoth"},
		{DisplayName: "AWSReserved"},   // ignored, AWS-only
		{DisplayName: "ManualGroup"},   // ignored, AWS-only
		{DisplayName: "DeleteMe"},      // not ignored, AWS-only -> delete
	}
	googleGroups := []*admin.Group{
		{Name: "GroupInBoth"},
		{Name: "NewGroup"},
	}

	add, del, eq := getGroupOperations(awsGroups, googleGroups, ignore)

	assert.Len(t, add, 1)
	assert.Equal(t, "NewGroup", add[0].DisplayName)
	assert.Len(t, del, 1)
	assert.Equal(t, "DeleteMe", del[0].DisplayName)
	assert.Len(t, eq, 1)
	assert.Equal(t, "GroupInBoth", eq[0].DisplayName)
}

func TestGetUserOperationsRespectsIgnore(t *testing.T) {
	ignore := func(name string) bool {
		return name == "ignored@example.com"
	}

	awsUsers := []*interfaces.User{
		{Username: "user@example.com", Active: true},
		{Username: "delete-me@example.com"},
		{Username: "ignored@example.com"},
	}
	googleUsers := []*admin.User{
		{
			PrimaryEmail: "user@example.com",
			Suspended:    false,
			Name:         &admin.UserName{GivenName: "Test", FamilyName: "User"},
		},
		{
			PrimaryEmail: "new-user@example.com",
			Suspended:    false,
			Name:         &admin.UserName{GivenName: "New", FamilyName: "User"},
		},
	}

	add, del, _, _ := getUserOperations(awsUsers, googleUsers, ignore)

	assert.Len(t, add, 1)
	assert.Equal(t, "new-user@example.com", add[0].Username)
	assert.Len(t, del, 1)
	assert.Equal(t, "delete-me@example.com", del[0].Username)
}

func TestGetOperationsNilIgnore(t *testing.T) {
	// Passing nil should not panic and should not skip any deletes.
	awsGroups := []*interfaces.Group{{DisplayName: "OnlyInAWS"}}
	_, del, _ := getGroupOperations(awsGroups, nil, nil)
	assert.Len(t, del, 1)

	awsUsers := []*interfaces.User{{Username: "only@aws"}}
	_, delU, _, _ := getUserOperations(awsUsers, nil, nil)
	assert.Len(t, delU, 1)
}
