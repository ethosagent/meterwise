package contract

import "reflect"

// DTO is a named API type; the generators walk its fields by reflection.
type DTO struct {
	Name string
	Type reflect.Type
}

// DTOs lists every API type exported to the web twin and openapi.json, in a
// fixed order. A new DTO is added here, nowhere else.
func DTOs() []DTO {
	return append(append(coreDTOs(), monitorDTOs()...), savingsDTOs()...)
}

// coreDTOs are the Plan 01-05 types.
func coreDTOs() []DTO {
	return []DTO{
		{"Health", reflect.TypeFor[Health]()},
		{"Version", reflect.TypeFor[Version]()},
		{"ErrorBody", reflect.TypeFor[ErrorBody]()},
		{"AppConfig", reflect.TypeFor[AppConfig]()},
		{"Preferences", reflect.TypeFor[Preferences]()},
		{"PreferencesPatch", reflect.TypeFor[PreferencesPatch]()},
		{"Me", reflect.TypeFor[Me]()},
		{"MeUser", reflect.TypeFor[MeUser]()},
		{"Workspace", reflect.TypeFor[WorkspaceView]()},
		{"WorkspaceList", reflect.TypeFor[WorkspaceList]()},
		{"CreateWorkspaceRequest", reflect.TypeFor[CreateWorkspaceRequest]()},
		{"UpdateWorkspaceRequest", reflect.TypeFor[UpdateWorkspaceRequest]()},
		{"SwitchWorkspaceResponse", reflect.TypeFor[SwitchWorkspaceResponse]()},
		{"Invitation", reflect.TypeFor[InvitationView]()},
		{"InvitationList", reflect.TypeFor[InvitationList]()},
		{"CreateInvitationRequest", reflect.TypeFor[CreateInvitationRequest]()},
		{"Grant", reflect.TypeFor[Grant]()},
		{"GrantList", reflect.TypeFor[GrantList]()},
		{"PermissionSnapshot", reflect.TypeFor[PermissionSnapshot]()},
		{"Member", reflect.TypeFor[Member]()},
		{"MemberList", reflect.TypeFor[MemberList]()},
		{"UpdateMemberRequest", reflect.TypeFor[UpdateMemberRequest]()},
		{"Group", reflect.TypeFor[Group]()},
		{"GroupList", reflect.TypeFor[GroupList]()},
		{"GroupRequest", reflect.TypeFor[GroupRequest]()},
		{"AttachWorkspaceRequest", reflect.TypeFor[AttachWorkspaceRequest]()},
		{"AddGroupMemberRequest", reflect.TypeFor[AddGroupMemberRequest]()},
		{"ReplaceGroupMembersRequest", reflect.TypeFor[ReplaceGroupMembersRequest]()},
		{"SetupSnippets", reflect.TypeFor[SetupSnippets]()},
		{"SetupStatus", reflect.TypeFor[SetupStatus]()},
		{"Setup", reflect.TypeFor[Setup]()},
		{"ResetURLRequest", reflect.TypeFor[ResetURLRequest]()},
		{"ResetURLResponse", reflect.TypeFor[ResetURLResponse]()},
		{"AllowedCIDRs", reflect.TypeFor[AllowedCIDRs]()},
		{"AuditActor", reflect.TypeFor[AuditActor]()},
		{"AuditEntry", reflect.TypeFor[AuditEntry]()},
		{"AuditList", reflect.TypeFor[AuditList]()},
		{"Session", reflect.TypeFor[SessionView]()},
		{"Call", reflect.TypeFor[CallView]()},
		{"SessionList", reflect.TypeFor[SessionList]()},
		{"SessionDetail", reflect.TypeFor[SessionDetail]()},
		{"StatsDay", reflect.TypeFor[StatsDayView]()},
		{"Stats", reflect.TypeFor[StatsView]()},
	}
}
