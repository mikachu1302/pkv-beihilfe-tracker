import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {SettingsDTO} from "../api";
import {settings} from "../test/fixtures";
import {renderAt, testStatus} from "../test/render";
import Settings, {Backups} from "./Settings";

const svc = vi.hoisted(() => ({
    BackupStatus: vi.fn(), SetBackupDir: vi.fn(), BackupNow: vi.fn(), GetSettings: vi.fn(), SaveSettings: vi.fn(), Reload: vi.fn(), SetDataDir: vi.fn(),
}));
const desktop = vi.hoisted(() => ({ChooseFolder: vi.fn(), OpenPath: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));

beforeEach(() => {
    vi.clearAllMocks();
    svc.BackupStatus.mockResolvedValue({dir: "", last: "", lastSize: 0, count: 0, error: ""});
    svc.GetSettings.mockResolvedValue(settings);
});

const tolerance = () => screen.getByLabelText("Toleranz für Klärungsbedarf (€)");

describe("Settings", () => {
    it("takes over settings of a newly chosen data folder when nothing was edited", async () => {
        const view = renderAt("/", "/", <Settings/>);
        expect(await screen.findByDisplayValue("1,00")).toBe(tolerance());
        svc.GetSettings.mockResolvedValue({...settings, clarifyTolerance: "5.00"});
        view.setApp({status: {...testStatus, dataDir: "/andere-daten"}});
        await waitFor(() => expect(tolerance()).toHaveValue("5,00"));
        expect(screen.queryByText(/inzwischen geändert/)).not.toBeInTheDocument();
    });

    it("keeps unsaved edits when the data changed elsewhere and offers to reload", async () => {
        const user = userEvent.setup();
        const view = renderAt("/", "/", <Settings/>);
        await screen.findByDisplayValue("1,00");
        await user.clear(tolerance());
        await user.type(tolerance(), "2,50");

        const newer: SettingsDTO = {...settings, clarifyTolerance: "5.00"};
        svc.GetSettings.mockResolvedValue(newer);
        view.setApp({dataVersion: 1});
        expect(await screen.findByText(/Die Einstellungen wurden inzwischen geändert/)).toBeInTheDocument();
        expect(tolerance()).toHaveValue("2,50");

        await user.click(screen.getByRole("button", {name: "Neu laden"}));
        expect(tolerance()).toHaveValue("5,00");
        expect(screen.queryByText(/inzwischen geändert/)).not.toBeInTheDocument();
    });

    it("saves with the settings the form was loaded from", async () => {
        const user = userEvent.setup();
        svc.SaveSettings.mockImplementation(async (d: SettingsDTO) => ({settings: {...d, clarifyTolerance: "2.50"}, problems: []}));
        renderAt("/", "/", <Settings/>);
        await screen.findByDisplayValue("1,00");
        await user.clear(tolerance());
        await user.type(tolerance(), "2,50");
        await user.click(screen.getByRole("button", {name: "Speichern"}));
        await waitFor(() => expect(svc.SaveSettings).toHaveBeenCalled());
        const [sent, original] = svc.SaveSettings.mock.calls[0];
        expect(sent.clarifyTolerance).toBe("2,50");
        expect(original).toEqual(settings);
        expect(await screen.findByText("Einstellungen gespeichert")).toBeInTheDocument();
    });

    it("saves with Ctrl+S", async () => {
        const user = userEvent.setup();
        svc.SaveSettings.mockImplementation(async (d: SettingsDTO) => ({settings: d, problems: []}));
        renderAt("/", "/", <Settings/>);
        await screen.findByDisplayValue("1,00");
        await user.clear(tolerance());
        await user.type(tolerance(), "3,00{Control>}s{/Control}");
        await waitFor(() => expect(svc.SaveSettings).toHaveBeenCalled());
        expect(svc.SaveSettings.mock.calls[0][0].clarifyTolerance).toBe("3,00");
    });
});

describe("Backups", () => {
    it("sets up a backup folder", async () => {
        const user = userEvent.setup();
        svc.BackupStatus.mockResolvedValue({dir: "", last: "", lastSize: 0, count: 0, error: ""});
        desktop.ChooseFolder.mockResolvedValue("/media/usb/sicherung");
        svc.SetBackupDir.mockResolvedValue({dir: "/media/usb/sicherung", last: "2026-11-02 10:15", lastSize: 2_500_000, count: 1, error: ""});
        renderAt("/", "/", <Backups onChanged={() => {}}/>);
        await user.click(await screen.findByRole("button", {name: "Sicherungsordner wählen …"}));
        await waitFor(() => expect(svc.SetBackupDir).toHaveBeenCalledWith("/media/usb/sicherung"));
        expect(await screen.findByText(/Letzte Sicherung 02.11.2026 10:15 \(2,4 MB\), 1 vorhanden/)).toBeInTheDocument();
    });

    it("backs up on request", async () => {
        const user = userEvent.setup();
        const st = {dir: "/media/usb/sicherung", last: "2026-11-02 10:15", lastSize: 1000, count: 1, error: ""};
        svc.BackupStatus.mockResolvedValue(st);
        svc.BackupNow.mockResolvedValue({...st, count: 2});
        renderAt("/", "/", <Backups onChanged={() => {}}/>);
        await user.click(await screen.findByRole("button", {name: "Jetzt sichern"}));
        expect(await screen.findByText(/2 vorhanden/)).toBeInTheDocument();
    });
});
