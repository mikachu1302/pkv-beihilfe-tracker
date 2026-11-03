import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {AISettingsDTO} from "../api";
import {setAI, useAI} from "../lib/ai";
import {renderAt} from "../test/render";
import {AISettingsSection} from "./SettingsAI";

const svc = vi.hoisted(() => ({SetAISettings: vi.fn(), SetAIKey: vi.fn(), TestAI: vi.fn()}));
vi.mock("../api", () => ({Svc: svc}));

const off: AISettingsDTO = {on: false, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: false, keyEnd: ""};

/** A promise the way Wails returns it (cancellable). */
function cancellable<T>(p: Promise<T>) {
    return Object.assign(p, {cancel: vi.fn()});
}

beforeEach(() => {
    vi.clearAllMocks();
    svc.SetAISettings.mockImplementation(async (a: AISettingsDTO) => ({...a, url: a.url.trim().replace(/\/?$/, "").replace(/:8080$/, ":8080/v1")}));
    svc.SetAIKey.mockImplementation(async (k: string) => ({...off, keySet: k !== "", keyEnd: k.length >= 12 ? k.slice(-4) : ""}));
});

function OnState() {
    return <span data-testid="on">{String(useAI()?.on)}</span>;
}

const section = () => screen.getByRole("region", {name: "KI"});

describe("Settings · KI", () => {
    it("switches the AI on and keeps the key write-only", async () => {
        const user = userEvent.setup();
        setAI(off);
        renderAt("/", "/", <><AISettingsSection/><OnState/></>);
        await user.click(within(section()).getByRole("checkbox", {name: "KI-Funktionen verwenden"}));
        expect(svc.SetAISettings).toHaveBeenCalledWith({...off, on: true});
        await waitFor(() => expect(screen.getByTestId("on")).toHaveTextContent("true"));

        const key = within(section()).getByLabelText("API-Schlüssel");
        expect(key).toHaveAttribute("type", "password");
        await user.type(key, "sk-0123456789abcdef7f3a{Enter}");
        expect(svc.SetAIKey).toHaveBeenCalledWith("sk-0123456789abcdef7f3a");
        expect(await within(section()).findByText("(…7f3a)")).toBeInTheDocument();
        expect(section().querySelector("input[type=password]")).toBeNull();

        await user.click(within(section()).getByRole("button", {name: "Entfernen"}));
        expect(svc.SetAIKey).toHaveBeenLastCalledWith("");
        expect(await within(section()).findByLabelText("API-Schlüssel")).toHaveValue("");
    });

    it("saves an edited address before testing and shows each step", async () => {
        const user = userEvent.setup();
        setAI({...off, on: true, keySet: true, keyEnd: "7f3a"});
        svc.TestAI.mockReturnValue(cancellable(Promise.resolve({ok: false, checks: [
            {ok: true, text: "Server erreichbar"},
            {ok: false, text: "Der KI-Server lehnt den API-Schlüssel ab (HTTP 401)"},
        ]})));
        renderAt("/", "/", <AISettingsSection/>);
        const url = within(section()).getByLabelText("Serveradresse");
        await user.clear(url);
        await user.type(url, "http://10.0.0.5:8080");
        await user.click(within(section()).getByRole("button", {name: "Verbindung testen"}));
        expect(svc.SetAISettings).toHaveBeenCalledWith(expect.objectContaining({url: "http://10.0.0.5:8080", on: true}));
        expect(await within(section()).findByText("Server erreichbar")).toBeInTheDocument();
        expect(within(section()).getByText(/lehnt den API-Schlüssel ab/)).toBeInTheDocument();
        expect(url).toHaveValue("http://10.0.0.5:8080/v1"); // as the backend tidied it up
        expect(within(section()).queryByRole("button", {name: "Übernehmen"})).not.toBeInTheDocument();
    });

    it("cancels a running test", async () => {
        const user = userEvent.setup();
        setAI({...off, on: true});
        const p = cancellable(new Promise(() => {}));
        svc.TestAI.mockReturnValue(p);
        renderAt("/", "/", <AISettingsSection/>);
        await user.click(within(section()).getByRole("button", {name: "Verbindung testen"}));
        expect(within(section()).getByRole("button", {name: "Verbindung wird getestet …"})).toBeDisabled();
        await user.click(within(section()).getByRole("button", {name: "Abbrechen"}));
        expect(p.cancel).toHaveBeenCalled();
        expect(within(section()).getByRole("button", {name: "Verbindung testen"})).toBeEnabled();
    });
});
