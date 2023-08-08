import { Divider, Tooltip } from "@mui/joy";
import { isEqual, uniqWith } from "lodash-es";
import { memo, useEffect, useRef, useState } from "react";
import { toast } from "react-hot-toast";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { UNKNOWN_ID } from "@/helpers/consts";
import { getRelativeTimeString } from "@/helpers/datetime";
import { useFilterStore, useMemoStore, useUserStore } from "@/store/module";
import { useMemoCacheStore, useUserV1Store } from "@/store/v1";
import { useTranslate } from "@/utils/i18n";
import showChangeMemoCreatedTsDialog from "./ChangeMemoCreatedTsDialog";
import { showCommonDialog } from "./Dialog/CommonDialog";
import Icon from "./Icon";
import MemoContent from "./MemoContent";
import showMemoEditorDialog from "./MemoEditor/MemoEditorDialog";
import MemoRelationListView from "./MemoRelationListView";
import MemoResourceListView from "./MemoResourceListView";
import showPreviewImageDialog from "./PreviewImageDialog";
import showShareMemo from "./ShareMemoDialog";
import UserAvatar from "./UserAvatar";
import "@/less/memo.less";

interface Props {
  memo: Memo;
  showCreator?: boolean;
  showFull?: boolean;
  showVisibility?: boolean;
  showRelatedMemos?: boolean;
  lazyRendering?: boolean;
}

const Memo: React.FC<Props> = (props: Props) => {
  const { memo, showCreator, showFull, showVisibility, showRelatedMemos, lazyRendering } = props;
  const { i18n } = useTranslation();
  const t = useTranslate();
  const filterStore = useFilterStore();
  const userStore = useUserStore();
  const memoStore = useMemoStore();
  const memoCacheStore = useMemoCacheStore();
  const userV1Store = useUserV1Store();
  const [shouldRender, setShouldRender] = useState<boolean>(lazyRendering ? false : true);
  const [createdTimeStr, setCreatedTimeStr] = useState<string>(getRelativeTimeString(memo.displayTs));
  const [relatedMemoList, setRelatedMemoList] = useState<Memo[]>([]);
  const memoContainerRef = useRef<HTMLDivElement>(null);
  const readonly = userStore.isVisitorMode() || userStore.getCurrentUsername() !== memo.creatorUsername;
  const creator = userV1Store.getUserByUsername(memo.creatorUsername);

  // Prepare memo creator.
  useEffect(() => {
    userV1Store.getOrFetchUserByUsername(memo.creatorUsername);
  }, [memo.creatorUsername]);

  // Prepare related memos.
  useEffect(() => {
    Promise.allSettled(memo.relationList.map((memoRelation) => memoCacheStore.getOrFetchMemoById(memoRelation.relatedMemoId))).then(
      (results) => {
        const memoList = [];
        for (const result of results) {
          if (result.status === "fulfilled") {
            memoList.push(result.value);
          }
        }
        setRelatedMemoList(uniqWith(memoList, isEqual));
      }
    );
  }, [memo.relationList]);

  // Update display time string.
  useEffect(() => {
    let intervalFlag: any = -1;
    if (Date.now() - memo.displayTs < 1000 * 60 * 60 * 24) {
      intervalFlag = setInterval(() => {
        setCreatedTimeStr(getRelativeTimeString(memo.displayTs));
      }, 1000 * 1);
    }

    return () => {
      clearInterval(intervalFlag);
    };
  }, [i18n.language]);

  // Lazy rendering.
  useEffect(() => {
    if (shouldRender) {
      return;
    }

    const root = document.body.querySelector("#root");
    if (root) {
      const checkShouldRender = () => {
        if (root.scrollTop + window.innerHeight > (memoContainerRef.current?.offsetTop || 0)) {
          setShouldRender(true);
          root.removeEventListener("scroll", checkShouldRender);
          return true;
        }
      };

      if (checkShouldRender()) {
        return;
      }
      root.addEventListener("scroll", checkShouldRender);
    }
  }, [lazyRendering]);

  if (!shouldRender) {
    // Render a placeholder to occupy the space.
    return <div className={`memo-wrapper min-h-[128px] ${"memos-" + memo.id}`} ref={memoContainerRef}></div>;
  }

  const handleTogglePinMemoBtnClick = async () => {
    try {
      if (memo.pinned) {
        await memoStore.unpinMemo(memo.id);
      } else {
        await memoStore.pinMemo(memo.id);
      }
    } catch (error) {
      // do nth
    }
  };

  const handleEditMemoClick = () => {
    showMemoEditorDialog({
      memoId: memo.id,
    });
  };

  const handleMarkMemoClick = () => {
    showMemoEditorDialog({
      relationList: [
        {
          memoId: UNKNOWN_ID,
          relatedMemoId: memo.id,
          type: "REFERENCE",
        },
      ],
    });
  };

  const handleArchiveMemoClick = async () => {
    try {
      await memoStore.patchMemo({
        id: memo.id,
        rowStatus: "ARCHIVED",
      });
    } catch (error: any) {
      console.error(error);
      toast.error(error.response.data.message);
    }
  };

  const handleDeleteMemoClick = async () => {
    showCommonDialog({
      title: t("memo.delete-memo"),
      content: t("memo.delete-confirm"),
      style: "warning",
      dialogName: "delete-memo-dialog",
      onConfirm: async () => {
        await memoStore.deleteMemoById(memo.id);
      },
    });
  };

  const handleGenerateMemoImageBtnClick = () => {
    showShareMemo(memo);
  };

  const handleMemoContentClick = async (e: React.MouseEvent) => {
    const targetEl = e.target as HTMLElement;

    if (targetEl.className === "tag-span") {
      const tagName = targetEl.innerText.slice(1);
      const currTagQuery = filterStore.getState().tag;
      if (currTagQuery === tagName) {
        filterStore.setTagFilter(undefined);
      } else {
        filterStore.setTagFilter(tagName);
      }
    } else if (targetEl.classList.contains("todo-block")) {
      if (readonly) {
        return;
      }

      const status = targetEl.dataset?.value;
      const todoElementList = [...(memoContainerRef.current?.querySelectorAll(`span.todo-block[data-value=${status}]`) ?? [])];
      for (const element of todoElementList) {
        if (element === targetEl) {
          const index = todoElementList.indexOf(element);
          const tempList = memo.content.split(status === "DONE" ? /- \[x\] / : /- \[ \] /);
          let finalContent = "";

          for (let i = 0; i < tempList.length; i++) {
            if (i === 0) {
              finalContent += `${tempList[i]}`;
            } else {
              if (i === index + 1) {
                finalContent += status === "DONE" ? "- [ ] " : "- [x] ";
              } else {
                finalContent += status === "DONE" ? "- [x] " : "- [ ] ";
              }
              finalContent += `${tempList[i]}`;
            }
          }
          await memoStore.patchMemo({
            id: memo.id,
            content: finalContent,
          });
        }
      }
    } else if (targetEl.tagName === "IMG") {
      const imgUrl = targetEl.getAttribute("src");
      if (imgUrl) {
        showPreviewImageDialog([imgUrl], 0);
      }
    }
  };

  const handleMemoContentDoubleClick = (e: React.MouseEvent) => {
    if (readonly) {
      return;
    }

    const loginUser = userStore.state.user;
    if (loginUser && !loginUser.localSetting.enableDoubleClickEditing) {
      return;
    }
    const targetEl = e.target as HTMLElement;

    if (targetEl.className === "tag-span") {
      return;
    } else if (targetEl.classList.contains("todo-block")) {
      return;
    }

    handleEditMemoClick();
  };

  const handleMemoCreatedTimeClick = (e: React.MouseEvent) => {
    if (e.altKey) {
      e.preventDefault();
      showChangeMemoCreatedTsDialog(memo.id);
    }
  };

  const handleMemoVisibilityClick = (visibility: Visibility) => {
    const currVisibilityQuery = filterStore.getState().visibility;
    if (currVisibilityQuery === visibility) {
      filterStore.setMemoVisibilityFilter(undefined);
    } else {
      filterStore.setMemoVisibilityFilter(visibility);
    }
  };

  return (
    <>
      <div className={`memo-wrapper ${"memos-" + memo.id} ${memo.pinned && !readonly ? "pinned" : ""}`} ref={memoContainerRef}>
        <div className="memo-top-wrapper">
          <div className="status-text-container">
            {showCreator && creator && (
              <>
                <Link className="flex flex-row justify-start items-center" to={`/u/${memo.creatorUsername}`}>
                  <UserAvatar className="!w-5 !h-auto mr-1" avatarUrl={creator.avatarUrl} />
                  <span className="text-sm text-gray-600 dark:text-zinc-300">{creator.nickname}</span>
                </Link>
                <Icon.Dot className="w-4 h-auto text-gray-400 dark:text-zinc-400" />
              </>
            )}
            <Link className="time-text" to={`/m/${memo.id}`} onClick={handleMemoCreatedTimeClick}>
              {createdTimeStr}
            </Link>
          </div>
          <div className="btns-container space-x-2">
            {showVisibility && memo.visibility !== "PRIVATE" && (
              <Tooltip title={t(`memo.visibility.${memo.visibility.toLowerCase() as Lowercase<typeof memo.visibility>}`)} placement="top">
                <div onClick={() => handleMemoVisibilityClick(memo.visibility)}>
                  {memo.visibility === "PUBLIC" ? (
                    <Icon.Globe2 className="w-4 h-auto cursor-pointer rounded text-green-600" />
                  ) : (
                    <Icon.Users className="w-4 h-auto cursor-pointer rounded text-gray-500 dark:text-gray-400" />
                  )}
                </div>
              </Tooltip>
            )}
            {memo.pinned && <Icon.Bookmark className="w-4 h-auto rounded text-green-600" />}
            {!readonly && (
              <>
                <span className="btn more-action-btn">
                  <Icon.MoreHorizontal className="icon-img" />
                </span>
                <div className="more-action-btns-wrapper">
                  <div className="more-action-btns-container min-w-[6em]">
                    <span className="btn" onClick={handleTogglePinMemoBtnClick}>
                      {memo.pinned ? <Icon.BookmarkMinus className="w-4 h-auto mr-2" /> : <Icon.BookmarkPlus className="w-4 h-auto mr-2" />}
                      {memo.pinned ? t("common.unpin") : t("common.pin")}
                    </span>
                    <span className="btn" onClick={handleEditMemoClick}>
                      <Icon.Edit3 className="w-4 h-auto mr-2" />
                      {t("common.edit")}
                    </span>
                    <span className="btn" onClick={handleGenerateMemoImageBtnClick}>
                      <Icon.Share className="w-4 h-auto mr-2" />
                      {t("common.share")}
                    </span>
                    <span className="btn" onClick={handleMarkMemoClick}>
                      <Icon.Link className="w-4 h-auto mr-2" />
                      {t("common.mark")}
                    </span>
                    <Divider className="!my-1" />
                    <span className="btn text-orange-500" onClick={handleArchiveMemoClick}>
                      <Icon.Archive className="w-4 h-auto mr-2" />
                      {t("common.archive")}
                    </span>
                    <span className="btn text-red-600" onClick={handleDeleteMemoClick}>
                      <Icon.Trash className="w-4 h-auto mr-2" />
                      {t("common.delete")}
                    </span>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>
        <MemoContent
          content={memo.content}
          showFull={showFull}
          onMemoContentClick={handleMemoContentClick}
          onMemoContentDoubleClick={handleMemoContentDoubleClick}
        />
        <MemoResourceListView resourceList={memo.resourceList} />
        {!showRelatedMemos && <MemoRelationListView relationList={memo.relationList} />}
      </div>

      {showRelatedMemos && relatedMemoList.length > 0 && (
        <>
          <p className="text-sm dark:text-gray-300 my-2 pl-4 opacity-50 flex flex-row items-center">
            <Icon.Link className="w-4 h-auto mr-1" />
            <span>Related memos</span>
          </p>
          {relatedMemoList.map((relatedMemo) => {
            return (
              <div key={relatedMemo.id} className="w-full">
                <Memo memo={relatedMemo} showCreator />
              </div>
            );
          })}
        </>
      )}
    </>
  );
};

export default memo(Memo);
