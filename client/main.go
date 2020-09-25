package main

import (
	"fmt"
	helpers "github.com/MSpaceDev/CelbuxHelpers"
)

type Member struct {
	PhoneNum string `json:"phoneNum"`
	Teams []string `json:"teams"`
	IsReg bool `json:"isReg"`
	IsMember bool `json:"isMember"`
}

type Team struct {
	FullyReg bool `json:"fullyReg"`
	TeamName string `json:"teamName"`
	ScoreId string `json:"scoreId"`
	InviteCode string `json:"inviteCode"`
	TeamMembers []string `json:"teamMembers"`
	TeamLeader string `json:"teamLeader"`
}

type Score struct {
	ScoreId string `json:"scoreId"`
	Score1 float64 `json:"score1"`
	Score2 float64 `json:"score2"`
	Score3 float64 `json:"score3"`
}

type UserInfo struct {
	Member *Member `json:"member"`
	Team   *Team `json:"team"`
	Score  *Score `json:"score"`
}

func main() {
	err := helpers.IntialiseClients("jiraonthego")
	if err != nil {
		_ = helpers.LogError(err)
	    return
	}

	inviteData := &UserInfo{
		Member: &Member{
			PhoneNum: "0648503047",
			Teams: []string{"Super Strikers","Super Fuckers","Super Crabs"},
		},
		Team: &Team{
			TeamName: "Super Fuckers",
			TeamMembers: []string{"0658462358","0548963287"},
			TeamLeader: "0648503047",
		},
	}

	fmt.Printf("Invite Data: %v\n\n", inviteData)

	var base64 string
	base64, err = helpers.StructToBase64(inviteData)
	if err != nil {
		_ = helpers.LogError(err)
		return
	}

	fmt.Printf("Base64: %v\n\n", base64)

	var newInviteData UserInfo
	err = helpers.Base64ToStruct(base64, &newInviteData)
	if err != nil {
		_ = helpers.LogError(err)
		return
	}

	fmt.Printf("New Invite Data: %v\n\n", newInviteData)
}