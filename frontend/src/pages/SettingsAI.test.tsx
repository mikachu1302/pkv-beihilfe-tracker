import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {AISettingsDTO} from "../api";
import {setAI, useAI} from "../lib/ai";
import {cancellable} from "../test/fixtures";
import {renderAt} from "../test/render";
import {AISettingsSection} from "./SettingsAI";

const svc = vi.hoisted(() => ({SetAISettings: vi.fn(), SetAIKey: vi.fn(), TestAI: vi.fn(), AIModels: vi.fn()}));
vi.mock("../api", () => ({Svc: svc}));

const off: AISettingsDTO = {on: false, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: false, keyEnd: ""};

beforeEach(() => {
    vi.clearAllMocks();
    svc.SetAISettings.mockImplementation(async (a: AISettingsDTO) => ({...a, url: a.url.trim().replace(/\/?$/, "").replace(/:8080$/, ":8080/v1")}));
    svc.SetAIKey.mockImplementation(async (k: string) => ({...off, keySet: k !== "", keyEnd: k.length >= 12 ? k.slice(-4) : ""}));
    svc.AIModels.mockImplementation(() => cancellable(Promise.resolve([]))); // no list: the name is typed
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
        expect(within(section()).getByText("Verbindung wird getestet …")).toBeInTheDocument();
        // The same button cancels: the focus stays on it.
        const cancel = within(section()).getByRole("button", {name: "Verbindungstest abbrechen"});
        expect(cancel).toHaveFocus();
        await user.click(cancel);
        expect(p.cancel).toHaveBeenCalled();
        expect(within(section()).getByRole("button", {name: "Verbindung testen"})).toHaveFocus();
    });

    it("offers the server's models that read images, and lets one type another", async () => {
        const user = userEvent.setup();
        setAI({...off, on: true, keySet: true, keyEnd: "7f3a"});
        svc.AIModels.mockImplementation(() => cancellable(Promise.resolve(["qwen3-vl-8b", "qwen3-vl-30b"])));
        renderAt("/", "/", <AISettingsSection/>);
        const choice = await within(section()).findByRole("combobox", {name: "Modell"});
        expect(choice).toHaveValue("qwen3-vl-8b");
        expect(within(choice).getAllByRole("option").map(o => o.textContent))
            .toEqual(["qwen3-vl-8b", "qwen3-vl-30b", "Anderes Modell eintragen …"]);

        await user.selectOptions(choice, "qwen3-vl-30b");
        expect(svc.SetAISettings).toHaveBeenLastCalledWith(expect.objectContaining({model: "qwen3-vl-30b"}));

        // Another name: the field comes (with the focus), and goes back to the list.
        await user.selectOptions(choice, "Anderes Modell eintragen …");
        const field = within(section()).getByRole("textbox", {name: "Modell"});
        expect(field).toHaveFocus();
        await user.clear(field);
        await user.type(field, "eigenes-modell{Enter}");
        expect(svc.SetAISettings).toHaveBeenLastCalledWith(expect.objectContaining({model: "eigenes-modell"}));
        await user.click(within(section()).getByRole("button", {name: "Aus der Liste wählen"}));
        const back = within(section()).getByRole("combobox", {name: "Modell"});
        expect(back).toHaveFocus();
        expect(back).toHaveValue("eigenes-modell");
        expect(within(back).getByRole("option", {name: "eigenes-modell – nicht in der Liste"})).toBeInTheDocument();
    });

    it("types the name without a list: AI off, server away – and asks again after a test", async () => {
        const user = userEvent.setup();
        setAI(off);
        renderAt("/", "/", <AISettingsSection/>);
        expect(within(section()).getByRole("textbox", {name: "Modell"})).toHaveValue("qwen3-vl-8b");
        expect(svc.AIModels).not.toHaveBeenCalled(); // nothing goes to the server while the AI is off

        svc.TestAI.mockReturnValue(cancellable(Promise.resolve({ok: true, checks: [{ok: true, text: "Server erreichbar"}]})));
        svc.AIModels.mockImplementation(() => cancellable(Promise.resolve(["qwen3-vl-8b"])));
        await user.click(within(section()).getByRole("button", {name: "Verbindung testen"}));
        expect(await within(section()).findByRole("combobox", {name: "Modell"})).toHaveValue("qwen3-vl-8b");
    });

    it("keeps the field when the list cannot be loaded", async () => {
        setAI({...off, on: true});
        svc.AIModels.mockImplementation(() => cancellable(Promise.reject(new Error("KI-Server nicht erreichbar"))));
        renderAt("/", "/", <AISettingsSection/>);
        await waitFor(() => expect(svc.AIModels).toHaveBeenCalled());
        expect(within(section()).getByRole("textbox", {name: "Modell"})).toHaveValue("qwen3-vl-8b");
        expect(within(section()).queryByRole("combobox")).not.toBeInTheDocument();
    });
});
