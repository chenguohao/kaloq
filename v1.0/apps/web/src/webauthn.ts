// W3C WebAuthn serialization with a manual fallback for browsers without the JSON helpers.
// https://www.w3.org/TR/webauthn-3/#sctn-parseCreationOptionsFromJSON
export function decodeURL(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  return Uint8Array.from(atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '=')), c => c.charCodeAt(0)).buffer
}
export function encodeURL(buffer: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(buffer))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
type DescriptorJSON = Omit<PublicKeyCredentialDescriptor, 'id'> & { id: string }
export type CreationJSON = Omit<PublicKeyCredentialCreationOptions, 'challenge' | 'user' | 'excludeCredentials'> & { challenge: string; user: Omit<PublicKeyCredentialUserEntity, 'id'> & { id: string }; excludeCredentials?: DescriptorJSON[] }
export type RequestJSON = Omit<PublicKeyCredentialRequestOptions, 'challenge' | 'allowCredentials'> & { challenge: string; allowCredentials?: DescriptorJSON[] }
function credentialJSON(credential: PublicKeyCredential) {
  const response = credential.response
  const common = { id: credential.id, rawId: encodeURL(credential.rawId), type: credential.type, authenticatorAttachment: credential.authenticatorAttachment, clientExtensionResults: credential.getClientExtensionResults() }
  if (response instanceof AuthenticatorAttestationResponse) return { ...common, response: { clientDataJSON: encodeURL(response.clientDataJSON), attestationObject: encodeURL(response.attestationObject), transports: response.getTransports?.() || [] } }
  const assertion = response as AuthenticatorAssertionResponse
  return { ...common, response: { clientDataJSON: encodeURL(assertion.clientDataJSON), authenticatorData: encodeURL(assertion.authenticatorData), signature: encodeURL(assertion.signature), userHandle: assertion.userHandle ? encodeURL(assertion.userHandle) : null } }
}
export function passkeyAvailable() { return window.isSecureContext && typeof PublicKeyCredential !== 'undefined' && !!navigator.credentials }
export async function registerCredential(options: CreationJSON) {
  const publicKey: PublicKeyCredentialCreationOptions = { ...options, challenge: decodeURL(options.challenge), user: { ...options.user, id: decodeURL(options.user.id) }, excludeCredentials: options.excludeCredentials?.map(c => ({ ...c, id: decodeURL(c.id) })) }
  const credential = await navigator.credentials.create({ publicKey })
  if (!(credential instanceof PublicKeyCredential)) throw new Error('未完成凭证登记，请重试。')
  return credentialJSON(credential)
}
export async function verifyCredential(options: RequestJSON) {
  const publicKey: PublicKeyCredentialRequestOptions = { ...options, challenge: decodeURL(options.challenge), allowCredentials: options.allowCredentials?.map(c => ({ ...c, id: decodeURL(c.id) })) }
  const credential = await navigator.credentials.get({ publicKey })
  if (!(credential instanceof PublicKeyCredential)) throw new Error('未完成凭证验证，请重试。')
  return credentialJSON(credential)
}
