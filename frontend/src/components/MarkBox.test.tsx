import {fireEvent, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {describe, expect, it, vi} from "vitest";
import {MarkBox} from "./MarkBox";

describe("MarkBox", () => {
    it("ticks without a date and shows the date once ticked", async () => {
        const user = userEvent.setup();
        const onToggle = vi.fn();
        const {rerender} = render(<MarkBox label="eingereicht" mark={{done: false, date: "", note: ""}} onToggle={onToggle}/>);
        await user.click(screen.getByLabelText("eingereicht"));
        expect(onToggle).toHaveBeenCalledWith(true);

        rerender(<MarkBox label="eingereicht" mark={{done: true, date: "2026-04-02", note: ""}} onToggle={onToggle} onDate={() => {}}/>);
        expect(screen.getByLabelText("eingereicht")).toBeChecked();
        expect(screen.getByRole("button", {name: "eingereicht: Datum 02.04.2026 ändern"})).toHaveTextContent("02.04.26");
    });

    it("lets the user change the date", async () => {
        const user = userEvent.setup();
        const onDate = vi.fn();
        render(<MarkBox label="erstattet" mark={{done: true, date: "2026-04-28", note: ""}} onToggle={() => {}} onDate={onDate}/>);
        await user.click(screen.getByRole("button", {name: /Datum 28.04.2026 ändern/}));
        const input = screen.getByLabelText("erstattet: Datum");
        fireEvent.change(input, {target: {value: "2026-04-30"}});
        fireEvent.blur(input);
        expect(onDate).toHaveBeenCalledWith("2026-04-30");
    });
});
