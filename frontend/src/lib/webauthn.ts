/* eslint-disable @typescript-eslint/no-explicit-any */
/**
 * WebAuthn client service for Passkey authentication.
 * Uses @simplewebauthn/browser for WebAuthn operations.
 */

import {
  startRegistration,
  startAuthentication,
} from '@simplewebauthn/browser';


import { api } from './api';
import { getStaticMessages } from '@/i18n/staticMessages';

/**
 * Register a new Passkey for the authenticated user.
 */
export async function registerPasskey(deviceName?: string): Promise<{ success: boolean; credentialId: number }> {
  // Get registration options from server
  const options = await api.settings.auth.webauthn.registrationOptions();

  let attResp: Awaited<ReturnType<typeof startRegistration>>;
  try {
    attResp = await startRegistration(options as any);
  } catch (error: any) {
    // User cancelled or browser doesn't support WebAuthn
    if (error.name === 'NotAllowedError') {
      throw new Error(getStaticMessages().settingsPasskeysData.registerFailed);
    }
    throw new Error(getWebAuthnErrorMessage(error));
  }

  // Verify registration with server
  const result = await api.settings.auth.webauthn.registrationVerify({
    credential: attResp as any,
    device_name: deviceName,
  });

  return { success: result.success, credentialId: result.credential_id };
}

/**
 * Authenticate using Passkey.
 */
export async function authenticateWithPasskey(username?: string): Promise<{ success: boolean; authenticated: boolean; username: string }> {
  // Get authentication options from server
  const options = await api.settings.auth.webauthn.authenticationOptions(username);

  // Start WebAuthn authentication ceremony
  let asseResp: Awaited<ReturnType<typeof startAuthentication>>;
  try {
    asseResp = await startAuthentication(options as any);
  } catch (error: any) {
    // User cancelled or browser doesn't support WebAuthn
    if (error.name === 'NotAllowedError') {
      throw new Error(getStaticMessages().auth.passkeyAuthenticationFailed);
    }
    throw new Error(getWebAuthnErrorMessage(error));
  }

  // Verify authentication with server
  const result = await api.settings.auth.webauthn.authenticationVerify({
    credential: asseResp as any,
  });

  return result;
}

/**
 * Check if WebAuthn is supported in current browser.
 */
export function isWebAuthnSupported(): boolean {
  return (
    window?.PublicKeyCredential !== undefined &&
    typeof window.PublicKeyCredential === 'function'
  );
}

/**
 * Check if platform authenticator (Touch ID, Face ID, Windows Hello) is available.
 */
export async function isPlatformAuthenticatorAvailable(): Promise<boolean> {
  if (!isWebAuthnSupported()) {
    return false;
  }

  try {
    return await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable();
  } catch {
    return false;
  }
}

/**
 * Get user-friendly error message for WebAuthn errors.
 */
export function getWebAuthnErrorMessage(error: any): string {
  const messages = getStaticMessages();
  if (error.name === 'NotAllowedError') {
    return messages.settingsPasskeysData.registerFailed;
  }
  if (error.name === 'NotSupportedError') {
    return messages.auth.browserNoPasskeys;
  }
  if (error.name === 'SecurityError') {
    return messages.auth.loginFailed;
  }
  if (error.name === 'InvalidStateError') {
    return messages.settingsAuthentication.unsupportedPasskeys;
  }
   return error.message || messages.common.unavailable;
}
