package auth

import "errors"

// AuthError 权限框架专用错误。
type AuthError struct {
	code    string
	message string
}

// Error 返回错误描述。
func (e *AuthError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}

// Code 返回错误码。
func (e *AuthError) Code() string {
	return e.code
}

// 预定义错误。
var (
	ErrSubjectNotFound    = &AuthError{code: "subject_not_found"}
	ErrRoleNotFound       = &AuthError{code: "role_not_found"}
	ErrPermissionNotFound = &AuthError{code: "permission_not_found"}
	ErrStorageRequired    = &AuthError{code: "storage_required"}
	ErrInvalidSubjectID   = &AuthError{code: "invalid_subject_id", message: "subject id is empty"}
	ErrInvalidRoleName    = &AuthError{code: "invalid_role_name", message: "role name is empty"}
	ErrInvalidPermission  = &AuthError{code: "invalid_permission", message: "resource or action is empty"}
)

// IsAuthError 判断是否为 AuthError。
func IsAuthError(err error) bool {
	var authErr *AuthError
	return errors.As(err, &authErr)
}
