// The AI settings of this device (internal/service/ai.go): loaded at start
// (App: DeviceSync) and after changes in the settings. The AI buttons and the
// "Fragen" page only show while the AI is switched on.
import type {AISettingsDTO} from "../api";
import {createStore, useStore} from "./store";

const ai = createStore<AISettingsDTO | null>(null);

/** Replaces the settings (after loading or changing them). */
export function setAI(a: AISettingsDTO | null) {
    ai.set(a);
}

/** The settings, or null while they are loading. */
export function useAI(): AISettingsDTO | null {
    return useStore(ai);
}

/** The AI is switched on on this device. */
export function useAIOn(): boolean {
    return useAI()?.on ?? false;
}

/** How the backend's message starts when the server cannot be reached (internal/ai.ErrUnreachable). */
export const AI_UNREACHABLE = "KI-Server nicht erreichbar";
