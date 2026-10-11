const eventDescriptions: Record<string, string> = {
	setup_link_issued: 'Setup link created',
	setup_completed: 'Login set up',
	setup_code_failed: 'Setup failed: wrong authenticator code',
	login_succeeded: 'Logged in',
	login_failed: 'Login failed',
	rate_limited: 'Logins paused after repeated failures',
	login_reset: 'Login reset with sudo tero reset-login',
	breach_check_skipped: 'Online breached-password check unavailable',
};

export function describeEvent(type: string): string {
	return eventDescriptions[type] ?? type;
}
