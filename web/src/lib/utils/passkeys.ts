import { beginPasskeyRegistration, finishPasskeyRegistration, type PasskeyInfo } from './api';

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

/**
 * Adds a passkey named name: the key service issues the options, the
 * browser's authenticator creates it, and the key service checks and stores
 * it. Needs a confirmed session.
 */
export async function registerPasskey(name: string, isEndingOthers: boolean): Promise<PasskeyInfo> {
	const { challengeId, options } = await beginPasskeyRegistration();
	const credential = await createPasskey(options);
	return finishPasskeyRegistration(challengeId, name, credential, isEndingOthers);
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

// After a login with the password and a code, the dashboard offers to add a
// passkey once. These flags live in the browser: whether to offer it on the
// next page, and whether the admin said "Not now" in this browser. Storage
// can be unavailable (private windows, blocked site data), so every access
// is guarded and the offer is simply skipped.
const offerKey = 'tero:offer-passkey';
const dismissedKey = 'tero:passkey-offer-dismissed';

/** Asks the next signed-in page to offer adding a passkey. */
export function requestPasskeyOffer(): void {
	try {
		sessionStorage.setItem(offerKey, '1');
	} catch {
		// No storage: no offer.
	}
}

/** Whether to offer a passkey now. Reading the request uses it up. */
export function takePasskeyOffer(): boolean {
	try {
		const isRequested = sessionStorage.getItem(offerKey) === '1';
		sessionStorage.removeItem(offerKey);
		return isRequested && localStorage.getItem(dismissedKey) !== '1' && isPasskeySupported();
	} catch {
		return false;
	}
}

/** Stops offering passkeys after login in this browser. */
export function dismissPasskeyOffer(): void {
	try {
		localStorage.setItem(dismissedKey, '1');
	} catch {
		// No storage: the offer comes back next login.
	}
}
