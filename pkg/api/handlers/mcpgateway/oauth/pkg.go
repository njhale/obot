package oauth

// TODO(cmcp): This package needs to be updated to handle composite mcp servers by:
// - managing oauth clients, oauth requests, tokens, and sessions for 1st and second level oauth for composite and component servers
// 	 - ensure these things are scoped correctly and that refresh works correctly
// - redirecting to the special authenticate/skip page when multiple component servers require 2nd level oauth
//   - we need to be sure that after all pending 2nd level oauth is skipped/finished, that the redirect to the initial 1st level composite client's redirect url happens
