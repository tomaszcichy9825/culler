// Single import point for the generated Wails bindings, so the deep relative
// path into frontend/bindings appears once rather than in every module.

export {
  ApplyService,
  ConfigService,
  DecisionService,
  LibraryService,
} from "../../bindings/github.com/tomaszcichy9825/culler/internal/app/index.js";

export type {
  ActionDTO,
  BatchDTO,
  DecisionItem,
  DirEntryDTO,
  FolderDTO,
  GroupDTO,
  PlanDTO,
  RatingItem,
  ResultDTO,
  VerdictItem,
} from "../../bindings/github.com/tomaszcichy9825/culler/internal/app/index.js";

export type { Config } from "../../bindings/github.com/tomaszcichy9825/culler/internal/config/index.js";
