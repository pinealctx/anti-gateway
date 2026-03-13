const TOKEN_KEY = "ag_admin_key";

export function getAdminKey(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setAdminKey(key: string) {
  localStorage.setItem(TOKEN_KEY, key);
}

export function clearAdminKey() {
  localStorage.removeItem(TOKEN_KEY);
}

export function isAuthenticated(): boolean {
  return getAdminKey().length > 0;
}
