// Passkeys go through the browser's WebAuthn API with the options the key
// service issued as JSON, and the answer goes back as JSON. The key service
// checks everything; the browser only carries the ceremony.

/** Whether this browser can create and use passkeys from JSON options. */
export function isPasskeySupported(): boolean {
	const hasPublicKeyCredential = typeof window !== 'undefined' && 'PublicKeyCredential' in window;
	if (!hasPublicKeyCredential) {
		return false;
	}

	return typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function';
}

/** Asks the authenticator for a new passkey and returns its answer as JSON. */
export async function createPasskey(options: unknown): Promise<unknown> {
	const publicKey = PublicKeyCredential.parseCreationOptionsFromJSON(options as PublicKeyCredentialCreationOptionsJSON);
	const credential = await navigator.credentials.create({ publicKey });
	return toJSON(credential);
}

/** Asks the authenticator to sign in with a passkey and returns its answer as JSON. */
export async function getPasskey(options: unknown): Promise<unknown> {
	const publicKey = PublicKeyCredential.parseRequestOptionsFromJSON(options as PublicKeyCredentialRequestOptionsJSON);
	const credential = await navigator.credentials.get({ publicKey });
	return toJSON(credential);
}

/** Whether the person dismissed the browser's passkey prompt, which isn't worth an error. */
export function isCancelled(error: unknown): boolean {
	return error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'AbortError');
}

function toJSON(credential: Credential | null): unknown {
	if (!(credential instanceof PublicKeyCredential)) {
		throw new DOMException('No passkey was returned', 'NotAllowedError');
	}

	return credential.toJSON();
}
