import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import {renderAt} from "../test/render";
import {Backups} from "./Settings";

const svc = vi.hoisted(() => ({BackupStatus: vi.fn(), SetBackupDir: vi.fn(), BackupNow: vi.fn()}));
const desktop = vi.hoisted(() => ({ChooseFolder: vi.fn(), OpenPath: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));

beforeEach(() => vi.clearAllMocks());

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
