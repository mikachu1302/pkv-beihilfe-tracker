// Changing the appearance settings of this device: applied right away and
// saved in the device configuration; a failure to save is reported, the
// change still applies until the next start.
import {Svc} from "../api";
import {useToast} from "../components/feedback";
import {patchAppearance, setSessionView, tableOptions, useAppearance} from "./appearance";
import type {InvoiceView, TableOptions} from "./appearance";
import {applyDesign} from "./design";
import type {Design} from "./design";
import {applyTheme} from "./theme";
import type {Theme} from "./theme";

/** Runs the backend call and reports its failure. */
function useSave() {
    const toast = useToast();
    return (call: Promise<void>) => {
        call.catch(toast.error);
    };
}

/** Sets the colour scheme of this device. */
export function useSetTheme(): (t: Theme) => void {
    const save = useSave();
    return t => {
        applyTheme(t);
        patchAppearance({theme: t});
        save(Svc.SetTheme(t));
    };
}

/** Sets the design of this device (which also closes the one-time hint about the new design). */
export function useSetDesign(): (d: Design) => void {
    const save = useSave();
    return d => {
        applyDesign(d);
        patchAppearance({design: d, designHint: false});
        save(Svc.SetDesign(d));
    };
}

/** Closes the one-time hint about the new design. */
export function useCloseDesignHint(): () => void {
    const save = useSave();
    return () => {
        patchAppearance({designHint: false});
        save(Svc.CloseDesignHint());
    };
}

/** Sets the default view of the invoice list; a quick choice made in the list gives way to it. */
export function useSetView(): (v: InvoiceView) => void {
    const save = useSave();
    return v => {
        patchAppearance({view: v});
        setSessionView(null);
        save(Svc.SetView(v));
    };
}

/** The options of the table view and a function to change them. */
export function useTableOptions(): [TableOptions, (o: TableOptions) => void] {
    const save = useSave();
    const options = tableOptions(useAppearance());
    return [options, o => {
        patchAppearance({table: o});
        save(Svc.SetTableOptions(o));
    }];
}
