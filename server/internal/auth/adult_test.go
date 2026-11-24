package auth

import "testing"

// TestViewAdultNeverDefault: no role, invite or service sign-in carries
// view_adult; requesters can never hold it; administrators need it too.
func TestViewAdultNeverDefault(t *testing.T) {
	for _, r := range []Role{RoleAdmin, RoleManager, RoleRequester} {
		if DefaultPermissions(r).Has(PermViewAdult) {
			t.Errorf("%s gets view_adult by default", r)
		}
	}
	if (PermViewAdult | PermManageLibrary).ForRole(RoleRequester).Has(PermViewAdult) {
		t.Error("requester kept view_adult")
	}
	if !(PermViewAdult).ForRole(RoleManager).Has(PermViewAdult) {
		t.Error("manager lost an explicit grant")
	}
	if (PermViewAdult | PermRequestMovies).WithoutAdult().Has(PermViewAdult) {
		t.Error("WithoutAdult")
	}
	admin := Principal{User: User{Role: RoleAdmin, Permissions: DefaultPermissions(RoleAdmin)}}
	if admin.Can(PermViewAdult) || admin.CanViewAdult() {
		t.Error("administrators must be granted view_adult explicitly")
	}
	admin.User.Permissions |= PermViewAdult
	if !admin.CanViewAdult() {
		t.Error("granted administrator")
	}
	key := admin
	key.APIKeyID = 3
	if key.CanViewAdult() {
		t.Error("API key without include adult")
	}
	key.IncludeAdult = true
	if !key.CanViewAdult() {
		t.Error("API key with include adult")
	}
	req := Principal{User: User{Role: RoleRequester, Permissions: PermViewAdult}}
	if req.CanViewAdult() {
		t.Error("requester sees adult content")
	}
}
