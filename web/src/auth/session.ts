import { create } from "zustand";

/** Where the API client gets a bearer token; set by the active auth adapter. */
type TokenGetter = () => Promise<string | null>;

interface SessionState {
  signedIn: boolean;
  /** True after an authenticated call came back 401: the session ended under us. */
  expired: boolean;
  getToken: TokenGetter;
  setSignedIn: (signedIn: boolean, getToken: TokenGetter) => void;
  markExpired: () => void;
  clearExpired: () => void;
}

const NO_TOKEN: TokenGetter = async () => null;

/** Auth session mirror for non-React code (the fetch layer). The adapter owns the truth. */
export const useSession = create<SessionState>((set) => ({
  signedIn: false,
  expired: false,
  getToken: NO_TOKEN,
  setSignedIn: (signedIn, getToken) =>
    set(signedIn ? { signedIn, getToken } : { signedIn, getToken: NO_TOKEN }),
  markExpired: () => set({ expired: true }),
  clearExpired: () => set({ expired: false }),
}));
