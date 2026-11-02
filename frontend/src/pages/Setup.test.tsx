import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import {renderAt, testStatus} from "../test/render";
import Setup from "./Setup";

const svc = vi.hoisted(() => ({SetDataDir: vi.fn(), Setup: vi.fn()}));
const desktop = vi.hoisted(() => ({ChooseFolder: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));

beforeEach(() => vi.clearAllMocks());

describe("Setup", () => {
    it("chooses the data folder first", async () => {
        const user = userEvent.setup();
        const reloadStatus = vi.fn();
        desktop.ChooseFolder.mockResolvedValue("/home/test/Gesundheit");
        svc.SetDataDir.mockResolvedValue({});
        renderAt("/", "/", <Setup/>, {status: {...testStatus, configured: false, dataDir: ""}, reloadStatus});
        await user.click(screen.getByRole("button", {name: "Durchsuchen …"}));
        expect(screen.getByLabelText("Datenordner")).toHaveValue("/home/test/Gesundheit");
        await user.click(screen.getByRole("button", {name: "Weiter"}));
        await waitFor(() => expect(svc.SetDataDir).toHaveBeenCalledWith("/home/test/Gesundheit"));
        expect(reloadStatus).toHaveBeenCalled();
    });

    it("sets up Beihilfe 70 % with the PKV taking the rest", async () => {
        const user = userEvent.setup();
        svc.Setup.mockResolvedValue({});
        renderAt("/", "/", <Setup/>, {status: {...testStatus, needsSetup: true}});
        await user.selectOptions(screen.getByLabelText("Bemessungssatz"), "70");
        expect(screen.getByText("Private Krankenversicherung (30 %)")).toBeInTheDocument();
        await user.type(screen.getByLabelText("Name der Versicherung"), "Muster PKV");
        await user.click(screen.getByLabelText(/Regeln des Bundes/));
        await user.click(screen.getByRole("button", {name: "Fertig"}));
        await waitFor(() => expect(svc.Setup).toHaveBeenCalled());
        expect(svc.Setup.mock.calls[0][0]).toMatchObject({withBeihilfe: true, beihilfeRate: "70", bundRules: true, pkvName: "Muster PKV"});
    });

    it("supports 100 % PKV", async () => {
        const user = userEvent.setup();
        svc.Setup.mockResolvedValue({});
        renderAt("/", "/", <Setup/>, {status: {...testStatus, needsSetup: true}});
        await user.click(screen.getByLabelText(/Nur private Krankenversicherung/));
        expect(screen.queryByLabelText("Bemessungssatz")).not.toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "Fertig"}));
        await waitFor(() => expect(svc.Setup.mock.calls[0][0]).toMatchObject({withBeihilfe: false}));
    });
});
