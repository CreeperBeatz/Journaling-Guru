import { useCallback, useEffect, useState } from "react";

// Recent-email history is purely a convenience: we cache addresses the user
// has previously typed into the sign-in form so the browser can offer them
// as <datalist> suggestions on the next visit. Stored client-side only —
// the backend already returns 200 for unknown emails, so this list never
// leaks "is this account real" info beyond what the user typed themselves.

const STORAGE_KEY = "journai:recent-emails";
const MAX_ENTRIES = 8;

function read(): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((x): x is string => typeof x === "string");
  } catch {
    return [];
  }
}

function write(entries: string[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(entries));
  } catch {
    // Quota / private mode — silently degrade. The form still works without
    // suggestions; nothing about auth depends on this cache.
  }
}

export function useRecentEmails() {
  const [recent, setRecent] = useState<string[]>([]);

  useEffect(() => {
    setRecent(read());
  }, []);

  const remember = useCallback((email: string) => {
    const cleaned = email.trim().toLowerCase();
    if (!cleaned) return;
    const next = [cleaned, ...read().filter((e) => e !== cleaned)].slice(0, MAX_ENTRIES);
    write(next);
    setRecent(next);
  }, []);

  const forget = useCallback((email: string) => {
    const next = read().filter((e) => e !== email);
    write(next);
    setRecent(next);
  }, []);

  return { recent, remember, forget };
}
